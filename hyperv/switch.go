package hyperv

import (
	"context"
	"fmt"

	"github.com/r4sd/go-wsman/wsman"
)

const (
	msvmExternalEthernetPortURI                   = nsVirtV2 + "/Msvm_ExternalEthernetPort"
	msvmVirtualEthernetSwitchSettingDataURI       = nsVirtV2 + "/Msvm_VirtualEthernetSwitchSettingData"
	msvmVirtualEthernetSwitchManagementServiceURI = nsVirtV2 + "/Msvm_VirtualEthernetSwitchManagementService"
)

// SwitchType は仮想スイッチの種別を表す。
//
// Hyper-V API には SwitchType フィールドが直接ないため、ResourceSettings の
// 構成 (External NIC binding の有無、Internal Port の有無) で実体を決める。
type SwitchType string

const (
	// SwitchTypePrivate: VM ↔ VM のみ。ホスト・外部から到達不可。
	SwitchTypePrivate SwitchType = "Private"

	// SwitchTypeInternal: VM ↔ VM ↔ ホスト。外部からは到達不可。
	SwitchTypeInternal SwitchType = "Internal"

	// SwitchTypeExternal: 物理 NIC 経由で外部ネットワークに接続。
	SwitchTypeExternal SwitchType = "External"
)

// CreateSwitchOptions は CreateSwitch のオプション。
type CreateSwitchOptions struct {
	// Name はスイッチの表示名 (一意である必要がある)。
	Name string

	// Type はスイッチ種別。
	Type SwitchType

	// ExternalAdapter は SwitchType=External の場合の物理 NIC 表示名。
	// ListExternalEthernetPorts で取得した ElementName を指定する。
	ExternalAdapter string

	// AllowManagementOS は External Switch でホスト OS にもアクセスを許可するかどうか。
	// true の場合、Internal Port が追加で作成される。
	AllowManagementOS bool

	// Notes はスイッチの備考。
	Notes string
}

// CreateSwitchResult は CreateSwitch の結果。
type CreateSwitchResult struct {
	SwitchRef string // 作成されたスイッチ識別子 (Msvm_VirtualEthernetSwitch.Name = GUID)
	JobRef    string
}

// ListExternalEthernetPorts は Hyper-V ホストの物理 NIC 一覧を返す。
//
// External Switch 作成時の接続先候補を確認するために使う。IsBound=true は
// 既に他のスイッチに紐付けされている (新規 External Switch では選択不可)。
func (c *Client) ListExternalEthernetPorts(ctx context.Context) ([]*Msvm_ExternalEthernetPort, error) {
	instances, err := c.wsman.Enumerate(ctx, msvmExternalEthernetPortURI)
	if err != nil {
		return nil, err
	}
	result := make([]*Msvm_ExternalEthernetPort, 0, len(instances))
	for _, inst := range instances {
		var p Msvm_ExternalEthernetPort
		if err := Unmarshal(inst.Properties(), &p); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_ExternalEthernetPort: %w", err)
		}
		result = append(result, &p)
	}
	return result, nil
}

// CreateSwitch は仮想スイッチを作成する。
//
// 内部で Msvm_VirtualEthernetSwitchManagementService.DefineSystem を呼び出す。
// SwitchType によって ResourceSettings の構成が変わる:
//   - Private: ResourceSettings なし
//   - Internal: ResourceSettings に Internal Port (HostResource なし) を 1 つ
//   - External: ResourceSettings に External NIC binding を 1 つ
//     (AllowManagementOS=true なら + Internal Port 1 つ)
func (c *Client) CreateSwitch(ctx context.Context, opts CreateSwitchOptions) (*CreateSwitchResult, error) {
	if opts.Name == "" {
		return nil, fmt.Errorf("CreateSwitch: Name must not be empty")
	}
	if opts.Type == "" {
		return nil, fmt.Errorf("CreateSwitch: Type must not be empty")
	}
	if opts.Type == SwitchTypeExternal && opts.ExternalAdapter == "" {
		return nil, fmt.Errorf("CreateSwitch: ExternalAdapter required for External switch")
	}

	// 1. SystemSettings の構築
	settings := &Msvm_VirtualEthernetSwitchSettingData{
		ElementName: opts.Name,
		Notes:       opts.Notes,
	}
	systemXML, err := marshalEmbeddedInstance(settings, "Msvm_VirtualEthernetSwitchSettingData", msvmVirtualEthernetSwitchSettingDataURI)
	if err != nil {
		return nil, fmt.Errorf("CreateSwitch: marshal system settings: %w", err)
	}

	// 2. ResourceSettings の組み立て (Type に応じて)
	resourceSettings, err := c.buildSwitchResourceSettings(ctx, opts)
	if err != nil {
		return nil, err
	}

	// 3. Invoke DefineSystem
	params := []wsman.Param{
		{Name: "SystemSettings", Value: systemXML},
	}
	for _, rs := range resourceSettings {
		params = append(params, wsman.Param{Name: "ResourceSettings", Value: rs})
	}

	resp, err := c.wsman.InvokeMulti(ctx, msvmVirtualEthernetSwitchManagementServiceURI, "DefineSystem", params)
	if err != nil {
		return nil, err
	}

	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return nil, fmt.Errorf("CreateSwitch: unexpected ReturnValue=%s", rv)
	}
	result := &CreateSwitchResult{
		SwitchRef: resp.Property("ResultingSystem"),
		JobRef:    resp.Property("Job"),
	}
	if rv == "4096" && result.JobRef == "" {
		return nil, fmt.Errorf("CreateSwitch: ReturnValue=4096 but no Job reference")
	}
	return result, nil
}

// buildSwitchResourceSettings は SwitchType に応じた ResourceSettings を返す。
//
// Private: 空配列
// Internal: Internal Port (HostResource なし) 1 個
// External: External NIC binding 1 個 + (AllowManagementOS なら Internal Port 1 個)
func (c *Client) buildSwitchResourceSettings(ctx context.Context, opts CreateSwitchOptions) ([]string, error) {
	switch opts.Type {
	case SwitchTypePrivate:
		return nil, nil

	case SwitchTypeInternal:
		internalPort, err := c.buildInternalPortAllocation(opts.Name)
		if err != nil {
			return nil, fmt.Errorf("CreateSwitch: build internal port: %w", err)
		}
		return []string{internalPort}, nil

	case SwitchTypeExternal:
		externalBinding, err := c.buildExternalAdapterBinding(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("CreateSwitch: build external binding: %w", err)
		}
		settings := []string{externalBinding}
		if opts.AllowManagementOS {
			internalPort, err := c.buildInternalPortAllocation(opts.Name)
			if err != nil {
				return nil, fmt.Errorf("CreateSwitch: build internal port: %w", err)
			}
			settings = append(settings, internalPort)
		}
		return settings, nil

	default:
		return nil, fmt.Errorf("CreateSwitch: unknown SwitchType %q", opts.Type)
	}
}

// buildInternalPortAllocation は Internal Port (ホスト OS との接続) を表す
// Msvm_EthernetPortAllocationSettingData の Embedded XML を返す。
//
// Internal Port は HostResource を持たない (物理 NIC に紐付かない)。
func (c *Client) buildInternalPortAllocation(switchName string) (string, error) {
	port := &Msvm_EthernetPortAllocationSettingData{
		ElementName:     switchName, // 慣習的にスイッチ名と同じ
		ResourceType:    ResourceTypeEthernetConnection,
		ResourceSubType: ResourceSubTypeEthernetConnection,
		EnabledState:    EnabledStateEnabled,
	}
	return marshalEmbeddedInstance(port, "Msvm_EthernetPortAllocationSettingData", msvmEthernetPortAllocationSettingDataURI)
}

// buildExternalAdapterBinding は External NIC へのバインディングを表す
// Msvm_EthernetPortAllocationSettingData の Embedded XML を返す。
//
// 内部で物理 NIC を表示名で検索し、その EPR を HostResource に埋め込む。
func (c *Client) buildExternalAdapterBinding(ctx context.Context, opts CreateSwitchOptions) (string, error) {
	ports, err := c.ListExternalEthernetPorts(ctx)
	if err != nil {
		return "", fmt.Errorf("list external ports: %w", err)
	}
	var match *Msvm_ExternalEthernetPort
	for _, p := range ports {
		if p.ElementName == opts.ExternalAdapter {
			match = p
			break
		}
	}
	if match == nil {
		return "", fmt.Errorf("external adapter %q not found", opts.ExternalAdapter)
	}

	nicEPR := buildEndpointReference(msvmExternalEthernetPortURI, map[string]string{
		"CreationClassName": "Msvm_ExternalEthernetPort",
		"Name":              match.Name,
	})
	binding := &Msvm_EthernetPortAllocationSettingData{
		ElementName:     opts.Name,
		ResourceType:    ResourceTypeEthernetConnection,
		ResourceSubType: ResourceSubTypeEthernetConnection,
		HostResource:    nicEPR,
		EnabledState:    EnabledStateEnabled,
	}
	return marshalEmbeddedInstance(binding, "Msvm_EthernetPortAllocationSettingData", msvmEthernetPortAllocationSettingDataURI)
}

// DestroySwitch は仮想スイッチを削除する。
//
// switchName は GetVirtualEthernetSwitch で取得した ElementName。
// 内部でスイッチ EPR を組み立てて Msvm_VirtualEthernetSwitchManagementService.DestroySystem を呼ぶ。
//
// VM が接続中の場合、削除は失敗する (先に NIC を Detach する必要がある)。
func (c *Client) DestroySwitch(ctx context.Context, switchName string) (string, error) {
	if switchName == "" {
		return "", fmt.Errorf("DestroySwitch: switchName must not be empty")
	}

	sw, err := c.GetVirtualEthernetSwitch(ctx, switchName)
	if err != nil {
		return "", fmt.Errorf("DestroySwitch: lookup: %w", err)
	}

	switchEPR := buildEndpointReference(msvmVirtualEthernetSwitchURI, map[string]string{
		"CreationClassName":       "Msvm_VirtualEthernetSwitch",
		"Name":                    sw.Name,
		"SystemCreationClassName": "Msvm_VirtualEthernetSwitch",
		"SystemName":              sw.Name,
	})

	resp, err := c.wsman.Invoke(ctx, msvmVirtualEthernetSwitchManagementServiceURI, "DestroySystem",
		map[string]string{"AffectedSystem": switchEPR})
	if err != nil {
		return "", err
	}
	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return "", fmt.Errorf("DestroySwitch: unexpected ReturnValue=%s", rv)
	}
	jobRef := resp.Property("Job")
	if rv == "4096" && jobRef == "" {
		return "", fmt.Errorf("DestroySwitch: ReturnValue=4096 but no Job reference")
	}
	return jobRef, nil
}
