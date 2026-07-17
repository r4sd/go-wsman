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
