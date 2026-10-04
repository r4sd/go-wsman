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
		if err := UnmarshalList(inst.PropertiesList(), &p); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_ExternalEthernetPort: %w", err)
		}
		result = append(result, &p)
	}
	return result, nil
}

// vesmsSelectors は Msvm_VirtualEthernetSwitchManagementService (シングルトン) の
// メソッド呼び出しに付与する SelectorSet を返す。
//
// 🔴 **これが無いと実機は InternalError を返す。** Hyper-V WMI プロバイダ
// (WsmWmiPl.dll) はメソッド実行時にインスタンスを特定する selector を要求する。
// VSMS (vsmsSelectors) と同じく selector で直るが、**症状は違う**
// (VSMS は WBEM_E_INVALID_METHOD_PARAMETERS、VESMS は InternalError)。
// スイッチ側は見落とされていた (#145)。
//
// 2026-10-05 実機確認 (Private スイッチ):
//
//	selector 無し          → WS-Man Fault [s:Receiver/w:InternalError]
//	CreationClassName 付き → DefineSystem は ReturnValue=0、DestroySystem は 4096
//
// VESMS のメソッド (DefineSystem / DestroySystem 等) すべてに付与すること。
func vesmsSelectors() []wsman.Selector {
	return []wsman.Selector{
		{Name: "CreationClassName", Value: "Msvm_VirtualEthernetSwitchManagementService"},
	}
}

// CreateSwitch は仮想スイッチを作成する。
//
// 内部で Msvm_VirtualEthernetSwitchManagementService.DefineSystem を呼び出す。
// SwitchType によって ResourceSettings の構成が変わる:
//   - Private: ResourceSettings なし
//   - Internal: ResourceSettings に Internal Port (HostResource = ホスト CS) を 1 つ
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

	resp, err := c.wsman.InvokeMulti(ctx, msvmVirtualEthernetSwitchManagementServiceURI, "DefineSystem", params, vesmsSelectors()...)
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
// Internal: Internal Port (HostResource = ホスト側 Msvm_ComputerSystem) 1 個
// External: External NIC binding 1 個 + (AllowManagementOS なら Internal Port 1 個)
func (c *Client) buildSwitchResourceSettings(ctx context.Context, opts CreateSwitchOptions) ([]string, error) {
	switch opts.Type {
	case SwitchTypePrivate:
		return nil, nil

	case SwitchTypeInternal:
		internalPort, err := c.buildInternalPortAllocation(ctx, opts.Name)
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
			internalPort, err := c.buildInternalPortAllocation(ctx, opts.Name)
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
// 🔴 **HostResource にホスト側 Msvm_ComputerSystem の WMI オブジェクトパスが必要** (#178)。
// 以前は「物理 NIC に紐付かないから HostResource 不要」としていたが誤りで、実機は
// ErrorCode=32773 を返していた。実機の Internal スイッチも同じパスを保存している。
//
// 2026-10-05 実機確認 (使い捨て Internal スイッチ):
//
//	HostResource 無し                → ErrorCode=32773 (Invalid parameter)
//	ホスト CS のパスを PROPERTY      → ErrorCode=32776 (Incorrect data type)
//	ホスト CS のパスを PROPERTY.ARRAY → 成功
func (c *Client) buildInternalPortAllocation(ctx context.Context, switchName string) (string, error) {
	host, err := c.hostComputerSystem(ctx)
	if err != nil {
		return "", fmt.Errorf("lookup host computer system: %w", err)
	}
	hostPath := wmiObjectPath(c.hostName, msvmComputerSystemURI, map[string]string{
		"CreationClassName": "Msvm_ComputerSystem",
		"Name":              host.Name,
	})
	port := &ethernetAllocationInput{
		ElementName:     switchName, // 慣習的にスイッチ名と同じ
		ResourceType:    ResourceTypeEthernetConnection,
		ResourceSubType: ResourceSubTypeEthernetConnection,
		HostResource:    []string{hostPath},
		EnabledState:    EnabledStateEnabled,
	}
	return marshalEmbeddedInstance(port, "Msvm_EthernetPortAllocationSettingData", msvmEthernetPortAllocationSettingDataURI)
}

// buildExternalAdapterBinding は External NIC へのバインディングを表す
// Msvm_EthernetPortAllocationSettingData の Embedded XML を返す。
//
// 内部で物理 NIC を表示名で検索し、その **WMI オブジェクトパス**を HostResource に埋め込む。
//
// キーは MOF の 4 つ (CreationClassName / DeviceID / SystemCreationClassName / SystemName)。
// 以前は CreationClassName + Name を EPR で入れていたが、Name はキーではない (#146)。
//
// ⚠️ **End-to-end では実機検証できていない。** 検証環境の物理 NIC は 1 枚で、既存スイッチに
// 束ねられている (IsBound=true)。束ねられた NIC への再バインドはキーが正しくても
// ErrorCode=32773 になるため、作成の成否で実装の正しさを判定できない。
// 根拠は**実機が保存している HostResource の値との一致**であって、作成の成功ではない。
// 配列化と WMI パス化そのものは #114 / #178 で実機検証済み。
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

	if match.DeviceID == "" || match.SystemName == "" {
		return "", fmt.Errorf("external adapter %q is missing MOF keys (DeviceID=%q SystemName=%q)",
			opts.ExternalAdapter, match.DeviceID, match.SystemName)
	}
	nicPath := wmiObjectPath(c.hostName, msvmExternalEthernetPortURI, map[string]string{
		"CreationClassName":       "Msvm_ExternalEthernetPort",
		"DeviceID":                match.DeviceID,
		"SystemCreationClassName": "Msvm_ComputerSystem",
		"SystemName":              match.SystemName,
	})
	binding := &ethernetAllocationInput{
		ElementName:     opts.Name,
		ResourceType:    ResourceTypeEthernetConnection,
		ResourceSubType: ResourceSubTypeEthernetConnection,
		HostResource:    []string{nicPath},
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

	// AddNetworkAdapter と同じ理由で Selector は CreationClassName + Name のみ。
	switchEPR := buildEndpointReference(msvmVirtualEthernetSwitchURI, map[string]string{
		"CreationClassName": "Msvm_VirtualEthernetSwitch",
		"Name":              sw.Name,
	})

	resp, err := c.wsman.Invoke(ctx, msvmVirtualEthernetSwitchManagementServiceURI, "DestroySystem",
		map[string]string{"AffectedSystem": switchEPR}, vesmsSelectors()...)
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
