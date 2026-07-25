package hyperv

import (
	"context"
	"fmt"
	"sort"

	"github.com/r4sd/go-wsman/wsman"
)

// 統合サービス (Integration Services) を表す 6 つの Component SettingData クラスの URI。
//
// 各クラスは CIM_ResourceAllocationSettingData を継承し、ElementName (表示名) と
// EnabledState (2=Enabled / 3=Disabled) を持つ。
//
// 表示名 (ElementName) は PowerShell Get-VMIntegrationService の Name と「同一の文字列」を返す。
// よって Read シャドウは PS 実装とロケールに関わらず同一結果になり、provider の refresh/plan
// パリティが保たれる。ただし ElementName は MOF が [AMENDMENT] 修飾子付きで、ホスト OS の
// 言語にローカライズされる (英語ホスト以外では別言語の文字列)。英語ホストでの値:
//
//	Msvm_HeartbeatComponentSettingData             → "Heartbeat"                (MOF 既定値あり)
//	Msvm_KvpExchangeComponentSettingData           → "Key-Value Pair Exchange"  (MOF 既定値あり)
//	Msvm_ShutdownComponentSettingData              → "Shutdown"                 (MOF 既定値あり)
//	Msvm_TimeSyncComponentSettingData              → "Time Synchronization"     (MOF 既定値あり)
//	Msvm_VssComponentSettingData                   → "VSS"                      (MOF 既定値あり)
//	Msvm_GuestServiceInterfaceComponentSettingData → "Guest Service Interface"  (MOF に既定値なし・実行時設定)
//
// 例: ドイツ語ホストでは "Gastdienstschnittstelle" 等 (dsccommunity/HyperVDsc #76)。
// ロケール非依存の識別が必要になったら InstanceID 末尾の component GUID かクラス名で判定する
// (書き込みプリミティブ #56 v2.1 で検討)。
//
// Source: https://learn.microsoft.com/en-us/windows/win32/hyperv_v2/msvm-*componentsettingdata
var integrationComponentURIs = []string{
	nsVirtV2 + "/Msvm_HeartbeatComponentSettingData",
	nsVirtV2 + "/Msvm_KvpExchangeComponentSettingData",
	nsVirtV2 + "/Msvm_ShutdownComponentSettingData",
	nsVirtV2 + "/Msvm_TimeSyncComponentSettingData",
	nsVirtV2 + "/Msvm_VssComponentSettingData",
	nsVirtV2 + "/Msvm_GuestServiceInterfaceComponentSettingData",
}

// IntegrationServiceComponent は 6 つの統合サービスのうちどれかを、MOF の ElementName 既定値
// (英語、ホスト OS ロケールに依存しない) で識別する型。
//
// 実行時の ElementName (ListIntegrationServices が返す IntegrationService.Name) はホスト OS
// 言語にローカライズされるが、この定数は CIM クラスを一意に選ぶための識別子であり、ローカライズ
// されない。SetIntegrationServiceEnabled への入力に使う (provider の Terraform config キーは
// 英語固定なので、この定数とそのまま 1:1 対応する)。
type IntegrationServiceComponent string

const (
	IntegrationServiceHeartbeat             IntegrationServiceComponent = "Heartbeat"
	IntegrationServiceKeyValuePairExchange  IntegrationServiceComponent = "Key-Value Pair Exchange"
	IntegrationServiceShutdown              IntegrationServiceComponent = "Shutdown"
	IntegrationServiceTimeSynchronization   IntegrationServiceComponent = "Time Synchronization"
	IntegrationServiceVSS                   IntegrationServiceComponent = "VSS"
	IntegrationServiceGuestServiceInterface IntegrationServiceComponent = "Guest Service Interface"
)

// integrationComponentClass は 1 コンポーネントの URI と CIM クラス名 (marshalEmbeddedInstance の
// className 引数用) を持つ。
type integrationComponentClass struct {
	uri       string
	className string
}

// integrationComponentByName は IntegrationServiceComponent → CIM クラス情報のルックアップ表。
var integrationComponentByName = map[IntegrationServiceComponent]integrationComponentClass{
	IntegrationServiceHeartbeat:             {nsVirtV2 + "/Msvm_HeartbeatComponentSettingData", "Msvm_HeartbeatComponentSettingData"},
	IntegrationServiceKeyValuePairExchange:  {nsVirtV2 + "/Msvm_KvpExchangeComponentSettingData", "Msvm_KvpExchangeComponentSettingData"},
	IntegrationServiceShutdown:              {nsVirtV2 + "/Msvm_ShutdownComponentSettingData", "Msvm_ShutdownComponentSettingData"},
	IntegrationServiceTimeSynchronization:   {nsVirtV2 + "/Msvm_TimeSyncComponentSettingData", "Msvm_TimeSyncComponentSettingData"},
	IntegrationServiceVSS:                   {nsVirtV2 + "/Msvm_VssComponentSettingData", "Msvm_VssComponentSettingData"},
	IntegrationServiceGuestServiceInterface: {nsVirtV2 + "/Msvm_GuestServiceInterfaceComponentSettingData", "Msvm_GuestServiceInterfaceComponentSettingData"},
}

// IntegrationService は VM の統合サービス 1 件の有効/無効状態を表す。
//
// Name は CIM ElementName (= PowerShell Get-VMIntegrationService の Name)。両者は同一文字列を
// 返すため Read パリティが保たれるが、ElementName はホスト OS 言語にローカライズされる点に注意
// (integrationComponentURIs のコメント参照)。Enabled は EnabledState==2 を true に写したもの。
type IntegrationService struct {
	Name    string
	Enabled bool
}

// integrationComponentSettingData は 6 つの Component SettingData クラスに共通する、
// Read に必要なフィールドだけを取り出す内部 struct。VM 単位の絞り込みに InstanceID を、
// 状態判定に EnabledState を、表示名に ElementName を使う。
type integrationComponentSettingData struct {
	InstanceID   string `cim:"InstanceID"`
	ElementName  string `cim:"ElementName"`
	EnabledState uint16 `cim:"EnabledState"`
}

// ListIntegrationServices は指定 VM の統合サービス状態を返す。
//
// vmGUID: 対象 VM の Msvm_ComputerSystem.Name (GUID)。
//
// 6 つの Component SettingData クラスをそれぞれ enumerate し、matchSettingDataVM で
// 対象 VM の InstanceID (Microsoft:<VM_GUID>\...) に属するものだけを残す。Hyper-V は
// WQL フィルタ列挙を拒否する (#80) ため無フィルタ列挙 + Go 側述語フィルタで絞る。
//
// PowerShell Get-VMIntegrationService と同じ表示名 (ElementName) と有効状態 (EnabledState==2)
// を返すため、provider の Read が PS 実装と同一結果を得られる。返り値は Name の昇順で安定化する。
func (c *Client) ListIntegrationServices(ctx context.Context, vmGUID string) ([]IntegrationService, error) {
	if vmGUID == "" {
		return nil, fmt.Errorf("ListIntegrationServices: vmGUID must not be empty")
	}

	result := make([]IntegrationService, 0, len(integrationComponentURIs))
	for _, uri := range integrationComponentURIs {
		instances, err := c.enumerateFiltered(ctx, uri, func(inst *wsman.Instance) bool {
			return matchSettingDataVM(inst.Property("InstanceID"), vmGUID)
		})
		if err != nil {
			return nil, fmt.Errorf("ListIntegrationServices: enumerate %s: %w", uri, err)
		}
		for _, inst := range instances {
			var comp integrationComponentSettingData
			if err := Unmarshal(inst.Properties(), &comp); err != nil {
				return nil, fmt.Errorf("ListIntegrationServices: unmarshal %s: %w", uri, err)
			}
			result = append(result, IntegrationService{
				Name:    comp.ElementName,
				Enabled: comp.EnabledState == EnabledStateEnabled,
			})
		}
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// resolveIntegrationServiceInstance は対象 VM 上の 1 コンポーネントの現行 SettingData
// (InstanceID・EnabledState 込み) を enumerate で解決する。GetIntegrationServiceEnabled /
// SetIntegrationServiceEnabled の共通処理。
func (c *Client) resolveIntegrationServiceInstance(ctx context.Context, vmGUID string, class integrationComponentClass) (*integrationComponentSettingData, error) {
	instances, err := c.enumerateFiltered(ctx, class.uri, func(inst *wsman.Instance) bool {
		return matchSettingDataVM(inst.Property("InstanceID"), vmGUID)
	})
	if err != nil {
		return nil, fmt.Errorf("enumerate %s: %w", class.uri, err)
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("%s not found for VM %q", class.className, vmGUID)
	}
	var comp integrationComponentSettingData
	if err := Unmarshal(instances[0].Properties(), &comp); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", class.className, err)
	}
	return &comp, nil
}

// GetIntegrationServiceEnabled は指定 VM の統合サービス 1 件の有効/無効状態を返す。
//
// ListIntegrationServices と異なり、ホスト OS 言語にローカライズされる実行時 ElementName に
// 依存せず component (ロケール非依存の識別子) で直接引く。SetIntegrationServiceEnabled の
// 差分なしガード(provider 側で書き込み省略の判定に使う想定、Slice A の processor と同じ設計)や、
// ローカライズホストでの単体確認に使う。
func (c *Client) GetIntegrationServiceEnabled(ctx context.Context, vmGUID string, component IntegrationServiceComponent) (bool, error) {
	if vmGUID == "" {
		return false, fmt.Errorf("GetIntegrationServiceEnabled: vmGUID must not be empty")
	}
	class, ok := integrationComponentByName[component]
	if !ok {
		return false, fmt.Errorf("GetIntegrationServiceEnabled: unknown component %q", component)
	}
	comp, err := c.resolveIntegrationServiceInstance(ctx, vmGUID, class)
	if err != nil {
		return false, fmt.Errorf("GetIntegrationServiceEnabled: %w", err)
	}
	return comp.EnabledState == EnabledStateEnabled, nil
}

// SetIntegrationServiceEnabled は指定 VM の統合サービス 1 件の EnabledState を変更する。
//
// PS 版 (Enable-VMIntegrationService / Disable-VMIntegrationService) をシャドウイングする。
// component は IntegrationServiceHeartbeat 等の定数で指定する (ローカライズされる実行時
// ElementName ではなく、CIM クラスを一意に選ぶロケール非依存の識別子)。
//
// 手順: ①対象コンポーネントの CIM クラスを対象 VM で enumerate し InstanceID を解決
// ②InstanceID + EnabledState のみを持つ最小 instance を組み立てて ModifyResourceSettings
// ③非同期 Job を WaitForJob で待つ。「変更箇所 + InstanceID のみ送る」原則 (#63/#97/#98 の
// ModifySystemSettings 最小 instance パターンと同型) により、ElementName 等の未変更フィールドは
// ゼロ値のまま送信されない。
//
// EnabledState (2=Enabled/3=Disabled) はどちらも Go のゼロ値 (0) を取らない値域のため、
// processor の bool フィールドで発覚したゼロ値ダウングレード黙殺 (Fable C、#88) は起きない —
// Disable (EnabledState=3) も非ゼロ値として正しく embedded instance に含まれる。
func (c *Client) SetIntegrationServiceEnabled(ctx context.Context, vmGUID string, component IntegrationServiceComponent, enabled bool) error {
	if vmGUID == "" {
		return fmt.Errorf("SetIntegrationServiceEnabled: vmGUID must not be empty")
	}
	class, ok := integrationComponentByName[component]
	if !ok {
		return fmt.Errorf("SetIntegrationServiceEnabled: unknown component %q", component)
	}
	comp, err := c.resolveIntegrationServiceInstance(ctx, vmGUID, class)
	if err != nil {
		return fmt.Errorf("SetIntegrationServiceEnabled: %w", err)
	}

	want := EnabledStateDisabled
	if enabled {
		want = EnabledStateEnabled
	}
	settings := &integrationComponentSettingData{
		InstanceID:   comp.InstanceID,
		EnabledState: want,
	}
	embedded, err := marshalEmbeddedInstance(settings, class.className, class.uri)
	if err != nil {
		return fmt.Errorf("SetIntegrationServiceEnabled: marshal: %w", err)
	}

	result, err := c.ModifyResourceSettings(ctx, []string{embedded})
	if err != nil {
		return fmt.Errorf("SetIntegrationServiceEnabled: %w", err)
	}
	if err := c.WaitForJob(ctx, result.JobRef); err != nil {
		return fmt.Errorf("SetIntegrationServiceEnabled: wait: %w", err)
	}
	return nil
}
