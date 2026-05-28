package hyperv

import (
	"context"
	"fmt"

	"github.com/r4sd/go-wsman/wsman"
)

const (
	msvmVirtualEthernetSwitchURI             = nsVirtV2 + "/Msvm_VirtualEthernetSwitch"
	msvmSyntheticEthernetPortSettingDataURI  = nsVirtV2 + "/Msvm_SyntheticEthernetPortSettingData"
	msvmEthernetPortAllocationSettingDataURI = nsVirtV2 + "/Msvm_EthernetPortAllocationSettingData"
	msvmEthernetSwitchPortVlanSettingDataURI = nsVirtV2 + "/Msvm_EthernetSwitchPortVlanSettingData"
)

// NetworkAdapterOptions は AddNetworkAdapter のオプション。
type NetworkAdapterOptions struct {
	// ElementName は NIC の表示名。
	ElementName string

	// SwitchName は接続先の仮想スイッチ表示名 (Msvm_VirtualEthernetSwitch.ElementName)。
	// 空文字の場合は NIC のみ追加し、スイッチには接続しない (無効 NIC)。
	SwitchName string

	// StaticMacAddress を true にすると Address フィールドの MAC が固定される。
	// false なら Hyper-V が動的に MAC を割り当てる (Address は無視される)。
	StaticMacAddress bool

	// MacAddress は固定 MAC アドレス (12 桁 hex、区切りなし。例: "00155D012345")。
	MacAddress string
}

// NetworkAdapterResult は AddNetworkAdapter の結果。
//
// 現行の Invoke レスポンスパーサ制約により ResultingResourceSettings は
// 単一値のみ取得可能。AddNetworkAdapter は内部で 2 回 AddResourceSettings を
// 行うため、PortRef は最初の呼び出しの結果、AllocationRef は 2 回目の結果を保持。
type NetworkAdapterResult struct {
	PortRef       string // 追加された Synthetic Ethernet Port の EPR (識別子)
	AllocationRef string // 追加された Ethernet Allocation の EPR (スイッチ接続時のみ)
	JobRef        string // 最後の AddResourceSettings の Job 参照
}

// ListVirtualEthernetSwitches は全仮想スイッチを取得する。
func (c *Client) ListVirtualEthernetSwitches(ctx context.Context) ([]*Msvm_VirtualEthernetSwitch, error) {
	instances, err := c.wsman.Enumerate(ctx, msvmVirtualEthernetSwitchURI)
	if err != nil {
		return nil, err
	}
	result := make([]*Msvm_VirtualEthernetSwitch, 0, len(instances))
	for _, inst := range instances {
		var sw Msvm_VirtualEthernetSwitch
		if err := Unmarshal(inst.Properties(), &sw); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_VirtualEthernetSwitch: %w", err)
		}
		result = append(result, &sw)
	}
	return result, nil
}

// GetVirtualEthernetSwitch は表示名で仮想スイッチを取得する。
//
// CIM の Selector では ElementName に直接アクセスできないため、List してから
// クライアント側でフィルタする。スイッチは通常数個しかないため負荷は無視できる。
func (c *Client) GetVirtualEthernetSwitch(ctx context.Context, switchName string) (*Msvm_VirtualEthernetSwitch, error) {
	if switchName == "" {
		return nil, fmt.Errorf("GetVirtualEthernetSwitch: switchName must not be empty")
	}
	switches, err := c.ListVirtualEthernetSwitches(ctx)
	if err != nil {
		return nil, err
	}
	for _, sw := range switches {
		if sw.ElementName == switchName {
			return sw, nil
		}
	}
	return nil, fmt.Errorf("GetVirtualEthernetSwitch: switch %q not found", switchName)
}

// ListNetworkAdapters は VM に紐づく NIC (Msvm_SyntheticEthernetPortSettingData) を返す。
//
// terraform の差分計算や、特定 NIC を identify したい場合に使う。
func (c *Client) ListNetworkAdapters(ctx context.Context, vmName string) ([]*Msvm_SyntheticEthernetPortSettingData, error) {
	if vmName == "" {
		return nil, fmt.Errorf("ListNetworkAdapters: vmName must not be empty")
	}
	query := fmt.Sprintf(
		`SELECT * FROM Msvm_SyntheticEthernetPortSettingData WHERE InstanceID LIKE '%s%s%%'`,
		settingDataInstanceIDPrefix, vmName,
	)
	instances, err := c.wsman.Enumerate(ctx, msvmSyntheticEthernetPortSettingDataURI, wsman.WithWQL(query))
	if err != nil {
		return nil, err
	}
	result := make([]*Msvm_SyntheticEthernetPortSettingData, 0, len(instances))
	for _, inst := range instances {
		var p Msvm_SyntheticEthernetPortSettingData
		if err := Unmarshal(inst.Properties(), &p); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_SyntheticEthernetPortSettingData: %w", err)
		}
		result = append(result, &p)
	}
	return result, nil
}

// AddNetworkAdapter は VM に NIC を追加し、必要なら指定スイッチに接続する。
//
// 内部で 2 段階の AddResourceSettings を実行する:
//  1. Msvm_SyntheticEthernetPortSettingData (NIC 本体)
//  2. Msvm_EthernetPortAllocationSettingData (スイッチ接続、SwitchName が指定された場合のみ)
//
// 1 が成功して 2 が失敗した場合、NIC は孤立状態 (スイッチ未接続) で残る。
// 呼び出し側は失敗時に RemoveNetworkAdapter で巻き戻すことを検討すること。
func (c *Client) AddNetworkAdapter(ctx context.Context, vmName string, opts NetworkAdapterOptions) (*NetworkAdapterResult, error) {
	if vmName == "" {
		return nil, fmt.Errorf("AddNetworkAdapter: vmName must not be empty")
	}
	if opts.ElementName == "" {
		return nil, fmt.Errorf("AddNetworkAdapter: opts.ElementName must not be empty")
	}
	if opts.StaticMacAddress && opts.MacAddress == "" {
		return nil, fmt.Errorf("AddNetworkAdapter: MacAddress required when StaticMacAddress=true")
	}

	port := &Msvm_SyntheticEthernetPortSettingData{
		ElementName:      opts.ElementName,
		ResourceType:     ResourceTypeEthernetAdapter,
		ResourceSubType:  ResourceSubTypeSyntheticEthernetPort,
		StaticMacAddress: opts.StaticMacAddress,
		Address:          opts.MacAddress,
	}
	portXML, err := marshalEmbeddedInstance(port, "Msvm_SyntheticEthernetPortSettingData", msvmSyntheticEthernetPortSettingDataURI)
	if err != nil {
		return nil, fmt.Errorf("AddNetworkAdapter: marshal port: %w", err)
	}

	portResult, err := c.AddResourceSettings(ctx, vmName, []string{portXML})
	if err != nil {
		return nil, fmt.Errorf("AddNetworkAdapter: add port: %w", err)
	}

	result := &NetworkAdapterResult{
		PortRef: portResult.ResultingResourceSettings,
		JobRef:  portResult.JobRef,
	}

	// スイッチに接続しない場合はここで終わり
	if opts.SwitchName == "" {
		return result, nil
	}

	// スイッチへの接続: Msvm_EthernetPortAllocationSettingData を追加
	sw, err := c.GetVirtualEthernetSwitch(ctx, opts.SwitchName)
	if err != nil {
		return result, fmt.Errorf("AddNetworkAdapter: lookup switch: %w", err)
	}

	switchEPR := buildEndpointReference(msvmVirtualEthernetSwitchURI, map[string]string{
		"Name":                    sw.Name,
		"CreationClassName":       "Msvm_VirtualEthernetSwitch",
		"SystemCreationClassName": "Msvm_VirtualSystemSettingData",
	})
	portEPR := buildEndpointReference(msvmSyntheticEthernetPortSettingDataURI, map[string]string{
		"InstanceID": result.PortRef,
	})

	allocation := &Msvm_EthernetPortAllocationSettingData{
		ElementName:     opts.ElementName,
		ResourceType:    ResourceTypeEthernetConnection,
		ResourceSubType: ResourceSubTypeEthernetConnection,
		HostResource:    switchEPR,
		Parent:          portEPR,
		EnabledState:    EnabledStateEnabled,
	}
	allocXML, err := marshalEmbeddedInstance(allocation, "Msvm_EthernetPortAllocationSettingData", msvmEthernetPortAllocationSettingDataURI)
	if err != nil {
		return result, fmt.Errorf("AddNetworkAdapter: marshal allocation: %w", err)
	}

	allocResult, err := c.AddResourceSettings(ctx, vmName, []string{allocXML})
	if err != nil {
		return result, fmt.Errorf("AddNetworkAdapter: add allocation: %w", err)
	}

	result.AllocationRef = allocResult.ResultingResourceSettings
	result.JobRef = allocResult.JobRef
	return result, nil
}

// RemoveNetworkAdapter は NIC を VM から削除する。
//
// adapterInstanceID は Msvm_SyntheticEthernetPortSettingData.InstanceID
// (ListNetworkAdapters の戻り値か AddNetworkAdapter の PortRef から取得)。
//
// スイッチ接続 (Msvm_EthernetPortAllocationSettingData) は NIC 本体の削除に
// 連鎖して Hyper-V 側で除去される。
func (c *Client) RemoveNetworkAdapter(ctx context.Context, adapterInstanceID string) (string, error) {
	if adapterInstanceID == "" {
		return "", fmt.Errorf("RemoveNetworkAdapter: adapterInstanceID must not be empty")
	}
	epr := buildEndpointReference(msvmSyntheticEthernetPortSettingDataURI, map[string]string{
		"InstanceID": adapterInstanceID,
	})
	return c.RemoveResourceSettings(ctx, []string{epr})
}

// AddNetworkAdapterVlan は NIC (Msvm_EthernetPortAllocationSettingData) に VLAN 設定を
// 追加する (#53)。CIM の AddFeatureSettings 経由で
// Msvm_EthernetSwitchPortVlanSettingData を Feature として紐付ける。
//
// adapterAllocationInstanceID は対象 NIC の Msvm_EthernetPortAllocationSettingData の
// InstanceID (AddNetworkAdapter の AllocationRef、または既存 NIC を Enumerate して取得)。
// 注意: SyntheticEthernetPort(NIC 本体) ではなく EthernetPortAllocation(スイッチ接続) の
// InstanceID であることに注意 — VLAN は「NIC とスイッチを繋ぐ port allocation」に紐付く。
//
// OperationMode によって意味を持つフィールドが切り替わる:
//
//   - Access: settings.OperationMode = VlanOperationModeAccess + AccessVlanId
//   - Trunk : settings.OperationMode = VlanOperationModeTrunk  + NativeVlanId + TrunkVlanIdArray
//   - Private: settings.OperationMode = VlanOperationModePrivate + PvlanMode + Primary/SecondaryVlanId
//
// 戻り値は非同期 Job 参照 (Msvm_ConcreteJob)。
// ReturnValue=0 (同期完了) の場合は空文字列、4096 (非同期開始) の場合は Job 参照を返す。
//
// CIM 仕様 (Microsoft 公式 MOF、AddFeatureSettings on Msvm_VirtualSystemManagementService):
//
//	uint32 AddFeatureSettings(
//	  [in]  Msvm_EthernetPortAllocationSettingData    REF AffectedConfiguration,
//	  [in]  string                                        FeatureSettings[],
//	  [out] Msvm_EthernetSwitchPortFeatureSettingData REF ResultingFeatureSettings[],
//	  [out] CIM_ConcreteJob                           REF Job
//	);
func (c *Client) AddNetworkAdapterVlan(ctx context.Context, adapterAllocationInstanceID string, settings *Msvm_EthernetSwitchPortVlanSettingData) (string, error) {
	if adapterAllocationInstanceID == "" {
		return "", fmt.Errorf("AddNetworkAdapterVlan: adapterAllocationInstanceID must not be empty")
	}
	if settings == nil {
		return "", fmt.Errorf("AddNetworkAdapterVlan: settings must not be nil")
	}

	vlanXML, err := marshalEmbeddedInstance(settings, "Msvm_EthernetSwitchPortVlanSettingData", nsVirtV2)
	if err != nil {
		return "", fmt.Errorf("AddNetworkAdapterVlan: marshal vlan settings: %w", err)
	}

	affectedEPR := buildEndpointReference(msvmEthernetPortAllocationSettingDataURI, map[string]string{
		"InstanceID": adapterAllocationInstanceID,
	})

	params := []wsman.Param{
		{Name: "AffectedConfiguration", Value: affectedEPR},
		{Name: "FeatureSettings", Value: vlanXML},
	}

	resp, err := c.wsman.InvokeMulti(ctx, msvmVirtualSystemManagementServiceURI, "AddFeatureSettings", params)
	if err != nil {
		return "", err
	}

	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return "", fmt.Errorf("AddNetworkAdapterVlan: unexpected ReturnValue=%s", rv)
	}

	jobRef := resp.Property("Job")
	if rv == "4096" && jobRef == "" {
		return "", fmt.Errorf("AddNetworkAdapterVlan: ReturnValue=4096 but no Job reference")
	}
	return jobRef, nil
}
