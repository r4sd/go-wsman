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
// EnabledState (2=Enabled / 3=Disabled) を持つ。表示名は PowerShell
// Get-VMIntegrationService の Name と一致する (MOF の ElementName 既定値で確認):
//
//	Msvm_HeartbeatComponentSettingData             → "Heartbeat"
//	Msvm_KvpExchangeComponentSettingData           → "Key-Value Pair Exchange"
//	Msvm_ShutdownComponentSettingData              → "Shutdown"
//	Msvm_TimeSyncComponentSettingData              → "Time Synchronization"
//	Msvm_VssComponentSettingData                   → "VSS"
//	Msvm_GuestServiceInterfaceComponentSettingData → "Guest Service Interface"
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
// Name は CIM ElementName (= PowerShell Get-VMIntegrationService の Name)。
// Enabled は EnabledState==2 を true に写したもの。
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
