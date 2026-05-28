package hyperv

import (
	"context"
	"fmt"

	"github.com/r4sd/go-wsman/wsman"
)

const (
	msvmVirtualSystemSettingDataURI       = nsVirtV2 + "/Msvm_VirtualSystemSettingData"
	msvmVirtualSystemManagementServiceURI = nsVirtV2 + "/Msvm_VirtualSystemManagementService"
)

// GetSystemSettingData は VM GUID から Realized 構成の SettingData を 1 件取得する。
//
// 同一 VM に対して Realized / Snapshot:Realized 等複数の SettingData が存在するため、
// VirtualSystemType="Microsoft:Hyper-V:System:Realized" を WQL でフィルタする。
//
// vmName は Msvm_ComputerSystem.Name（VM GUID）。
// 該当する Realized 設定が見つからない場合はエラーを返す。
func (c *Client) GetSystemSettingData(ctx context.Context, vmName string) (*Msvm_VirtualSystemSettingData, error) {
	if vmName == "" {
		return nil, fmt.Errorf("GetSystemSettingData: vmName must not be empty")
	}
	query := fmt.Sprintf(
		`SELECT * FROM Msvm_VirtualSystemSettingData WHERE VirtualSystemIdentifier="%s" AND VirtualSystemType="%s"`,
		vmName, VirtualSystemTypeRealized,
	)

	instances, err := c.wsman.Enumerate(ctx, msvmVirtualSystemSettingDataURI, wsman.WithWQL(query))
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("GetSystemSettingData: no Realized setting found for VM %q", vmName)
	}

	var settings Msvm_VirtualSystemSettingData
	if err := UnmarshalList(instances[0].PropertiesList(), &settings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Msvm_VirtualSystemSettingData: %w", err)
	}
	return &settings, nil
}

// ListSystemSettingData は全 VM の Realized 構成 SettingData を取得する。
//
// Snapshot:Realized 等は除外し、各 VM の現在構成のみを返す。
func (c *Client) ListSystemSettingData(ctx context.Context) ([]*Msvm_VirtualSystemSettingData, error) {
	query := fmt.Sprintf(
		`SELECT * FROM Msvm_VirtualSystemSettingData WHERE VirtualSystemType="%s"`,
		VirtualSystemTypeRealized,
	)

	instances, err := c.wsman.Enumerate(ctx, msvmVirtualSystemSettingDataURI, wsman.WithWQL(query))
	if err != nil {
		return nil, err
	}

	result := make([]*Msvm_VirtualSystemSettingData, 0, len(instances))
	for _, inst := range instances {
		var settings Msvm_VirtualSystemSettingData
		if err := UnmarshalList(inst.PropertiesList(), &settings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_VirtualSystemSettingData: %w", err)
		}
		result = append(result, &settings)
	}
	return result, nil
}

// DefineSystemResult は DefineSystem の戻り値を表す。
//
// ResultingSystem は作成された VM の参照を表す。EPR の Selector "Name" の値
// (= VM GUID) が抽出されて格納される。同期成功時は即取得可能、非同期 Job 完了後は
// Job が完了してから VM が確定する。
type DefineSystemResult struct {
	JobRef          string // 非同期 Job 参照 (Msvm_ConcreteJob の InstanceID)。同期成功時は空。
	ResultingSystem string // 作成された VM の識別子 (Msvm_ComputerSystem.Name = VM GUID)
	ReturnValue     string // "0"=同期成功, "4096"=非同期 Job 開始
}

// DefineSystem は新規 VM を作成する。
//
// settings には少なくとも以下を設定すること:
//   - ElementName: VM 表示名
//   - VirtualSystemSubType: Generation (VirtualSystemSubTypeGen1 or :Gen2)
//   - 必要に応じて ConfigurationDataRoot, AutomaticStartupAction 等
//
// VirtualSystemType / SystemType は Hyper-V 側で自動的に Realized が割り当てられる
// ため、settings に明示する必要はない。
//
// ResourceSettings (NIC/Disk/Memory/CPU 等) は Phase 4 で対応するため、ここでは
// 受け付けない。VM 作成後に AddResourceSettings 等で追加する設計。
//
// 戻り値の ReturnValue: "0"=同期成功, "4096"=非同期 Job 開始。
// 4096 の場合、Job 完了まで VM の準備は未完了。
func (c *Client) DefineSystem(ctx context.Context, settings *Msvm_VirtualSystemSettingData) (*DefineSystemResult, error) {
	if settings == nil {
		return nil, fmt.Errorf("DefineSystem: settings must not be nil")
	}
	if settings.ElementName == "" {
		return nil, fmt.Errorf("DefineSystem: settings.ElementName must not be empty")
	}

	embedded, err := marshalEmbeddedInstance(settings, "Msvm_VirtualSystemSettingData", msvmVirtualSystemSettingDataURI)
	if err != nil {
		return nil, fmt.Errorf("DefineSystem: marshal failed: %w", err)
	}

	resp, err := c.wsman.Invoke(ctx, msvmVirtualSystemManagementServiceURI, "DefineSystem",
		map[string]string{"SystemSettings": embedded})
	if err != nil {
		return nil, err
	}

	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return nil, fmt.Errorf("DefineSystem: unexpected ReturnValue=%s", rv)
	}

	result := &DefineSystemResult{
		JobRef:          resp.Property("Job"),
		ResultingSystem: resp.Property("ResultingSystem"),
		ReturnValue:     rv,
	}
	if rv == "4096" && result.JobRef == "" {
		return nil, fmt.Errorf("DefineSystem: ReturnValue=4096 but no Job reference")
	}
	return result, nil
}

// DestroySystem は VM を削除する。
//
// vmName は Msvm_ComputerSystem.Name (VM GUID)。VM が起動中の場合、削除は失敗する
// (事前に RequestStateChange で停止する必要がある。Phase 3 part 3 で対応)。
//
// 戻り値は非同期 Job 参照。ReturnValue=4096 の場合は Job 完了まで削除は未完了。
func (c *Client) DestroySystem(ctx context.Context, vmName string) (string, error) {
	if vmName == "" {
		return "", fmt.Errorf("DestroySystem: vmName must not be empty")
	}

	affected := buildEndpointReference(msvmComputerSystemURI, map[string]string{
		"Name": vmName,
	})

	resp, err := c.wsman.Invoke(ctx, msvmVirtualSystemManagementServiceURI, "DestroySystem",
		map[string]string{"AffectedSystem": affected})
	if err != nil {
		return "", err
	}

	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return "", fmt.Errorf("DestroySystem: unexpected ReturnValue=%s", rv)
	}

	jobRef := resp.Property("Job")
	if rv == "4096" && jobRef == "" {
		return "", fmt.Errorf("DestroySystem: ReturnValue=4096 but no Job reference")
	}
	return jobRef, nil
}

// UpdateVm は VM の構成設定を CIM の ModifySystemSettings 経由で更新する (#50 part 2/2)。
//
// settings.InstanceID で対象 VM を特定する。`marshalEmbeddedInstance` はゼロ値の
// フィールドを出力しないため (CIM SettingData の慣習 = 未指定 = デフォルト/変更なし)、
// 変更したいフィールドだけ書き換えて渡せばよい。典型的な使い方:
//
//	settings, err := c.GetSystemSettingData(ctx, vmName)
//	if err != nil { return err }
//	settings.Notes = []string{"updated"}
//	settings.AutomaticCriticalErrorAction = AutomaticCriticalErrorActionPause
//	jobRef, err := c.UpdateVm(ctx, settings)
//
// 戻り値は非同期 Job 参照 (Msvm_ConcreteJob)。
// ReturnValue=0 (同期完了) の場合は空文字列、4096 (非同期開始) の場合は Job 参照を返す。
//
// CIM 仕様 (Microsoft 公式 MOF、ModifySystemSettings on Msvm_VirtualSystemManagementService):
//
//	uint32 ModifySystemSettings(
//	  [in]  string              SystemSettings,
//	  [out] CIM_ConcreteJob REF Job
//	);
func (c *Client) UpdateVm(ctx context.Context, settings *Msvm_VirtualSystemSettingData) (string, error) {
	if settings == nil {
		return "", fmt.Errorf("UpdateVm: settings must not be nil")
	}
	if settings.InstanceID == "" {
		return "", fmt.Errorf("UpdateVm: settings.InstanceID must not be empty (used to identify the VM)")
	}

	embedded, err := marshalEmbeddedInstance(settings, "Msvm_VirtualSystemSettingData", nsVirtV2)
	if err != nil {
		return "", fmt.Errorf("UpdateVm: marshal embedded instance: %w", err)
	}

	resp, err := c.wsman.Invoke(ctx, msvmVirtualSystemManagementServiceURI, "ModifySystemSettings",
		map[string]string{
			"SystemSettings": embedded,
		})
	if err != nil {
		return "", err
	}

	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return "", fmt.Errorf("UpdateVm: unexpected ReturnValue=%s", rv)
	}

	jobRef := resp.Property("Job")
	if rv == "4096" && jobRef == "" {
		return "", fmt.Errorf("UpdateVm: ReturnValue=4096 but no Job reference")
	}
	return jobRef, nil
}
