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

// matchRealizedSettingDataForVM は SettingData が指定 VM の Realized 構成かを判定する純関数。
//
// 元の WQL `VirtualSystemIdentifier="<vm>" AND VirtualSystemType="Realized"` と同じ絞り込み。
// VirtualSystemIdentifier は VM GUID と完全一致 (実機の GUID は常に同一表記)。
func matchRealizedSettingDataForVM(vmIdentifier, vmType, vmName string) bool {
	return vmIdentifier == vmName && vmType == VirtualSystemTypeRealized
}

// matchRealizedSettingData は SettingData が Realized 構成かを判定する純関数。
//
// 元の WQL `VirtualSystemType="Realized"` 相当 (Snapshot:Realized 等を除外)。
func matchRealizedSettingData(vmType string) bool {
	return vmType == VirtualSystemTypeRealized
}

// GetSystemSettingData は VM GUID から Realized 構成の SettingData を 1 件取得する。
//
// 同一 VM に対して Realized / Snapshot:Realized 等複数の SettingData が存在するため、
// VirtualSystemType="Microsoft:Hyper-V:System:Realized" でフィルタする。
//
// Hyper-V は WS-Man の WQL フィルタ列挙を拒否する (#80) ため、無フィルタ列挙 + Go 側
// フィルタ (matchRealizedSettingDataForVM) で同じ絞り込みを行う。
//
// vmName は Msvm_ComputerSystem.Name（VM GUID）。
// 該当する Realized 設定が見つからない場合はエラーを返す。
func (c *Client) GetSystemSettingData(ctx context.Context, vmName string) (*Msvm_VirtualSystemSettingData, error) {
	if vmName == "" {
		return nil, fmt.Errorf("GetSystemSettingData: vmName must not be empty")
	}

	instances, err := c.enumerateFiltered(ctx, msvmVirtualSystemSettingDataURI, func(inst *wsman.Instance) bool {
		return matchRealizedSettingDataForVM(inst.Property("VirtualSystemIdentifier"), inst.Property("VirtualSystemType"), vmName)
	})
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
// Hyper-V は WQL フィルタ列挙を拒否する (#80) ため、無フィルタ列挙 + Go 側フィルタ
// (matchRealizedSettingData) で VirtualSystemType=Realized を絞り込む。
func (c *Client) ListSystemSettingData(ctx context.Context) ([]*Msvm_VirtualSystemSettingData, error) {
	instances, err := c.enumerateFiltered(ctx, msvmVirtualSystemSettingDataURI, func(inst *wsman.Instance) bool {
		return matchRealizedSettingData(inst.Property("VirtualSystemType"))
	})
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

// vsmsSelectors は Msvm_VirtualSystemManagementService (シングルトン) のメソッド呼び出しに
// 付与する SelectorSet を返す。Hyper-V WMI プロバイダ (WsmWmiPl.dll) はメソッド実行時に
// インスタンスを特定する selector を要求し、無いと WBEM_E_INVALID_METHOD_PARAMETERS
// (HRESULT 0x8004102F) になる。CreationClassName だけでシングルトンを一意特定できる
// (libvirt の hyperv driver 実装で実証、実機 acc test で確認)。
//
// VSMS のメソッド (DefineSystem / DestroySystem / ModifySystemSettings /
// Add・Modify・RemoveResourceSettings 等) すべてに付与すること。
func vsmsSelectors() []wsman.Selector {
	return []wsman.Selector{
		{Name: "CreationClassName", Value: "Msvm_VirtualSystemManagementService"},
	}
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
		map[string]string{"SystemSettings": embedded}, vsmsSelectors()...)
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
		map[string]string{"AffectedSystem": affected}, vsmsSelectors()...)
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
//
// clearReadOnlyForModify は ModifySystemSettings に渡してはいけない read-only プロパティを
// ゼロ値にする。InstanceID (キー) は対象 VM 特定に必須なので残す。read-only 値を送り返すと
// Hyper-V がジョブを Exception で失敗させる。Read/Write 修飾子は MS 公式 MOF
// (Msvm_VirtualSystemSettingData) に準拠。ElementName / Notes / Automatic*Action /
// MMIO / BootSourceOrder 等の変更可能フィールドは保持する。
func clearReadOnlyForModify(sd *Msvm_VirtualSystemSettingData) {
	sd.VirtualSystemIdentifier = ""
	sd.VirtualSystemType = ""
	sd.VirtualSystemSubType = ""
	sd.ConfigurationID = ""
	sd.ConfigurationDataRoot = ""
	sd.ConfigurationFile = ""
	sd.SnapshotDataRoot = ""
	sd.SuspendDataRoot = ""
	sd.SwapFileDataRoot = ""
	sd.LogDataRoot = ""
	sd.CreationTime = ""
	sd.Version = ""
	sd.Caption = ""
	sd.Description = ""
}

func (c *Client) UpdateVm(ctx context.Context, settings *Msvm_VirtualSystemSettingData) (string, error) {
	if settings == nil {
		return "", fmt.Errorf("UpdateVm: settings must not be nil")
	}
	if settings.InstanceID == "" {
		return "", fmt.Errorf("UpdateVm: settings.InstanceID must not be empty (used to identify the VM)")
	}

	// ModifySystemSettings は「変更箇所 (modified aspects) + InstanceID」だけを受け付ける。
	// GetSystemSettingData の結果をそのまま渡すと read-only プロパティ
	// (VirtualSystemType="...:Realized" / CreationTime / Version / Configuration* 等) が
	// 非ゼロ値で乗り、ジョブが Exception で失敗する (実機 acc test で確認)。コピーを作って
	// read-only をクリアしてから marshal する (呼び出し側の struct は変更しない)。
	mod := *settings
	clearReadOnlyForModify(&mod)

	embedded, err := marshalEmbeddedInstance(&mod, "Msvm_VirtualSystemSettingData", nsVirtV2)
	if err != nil {
		return "", fmt.Errorf("UpdateVm: marshal embedded instance: %w", err)
	}

	resp, err := c.wsman.Invoke(ctx, msvmVirtualSystemManagementServiceURI, "ModifySystemSettings",
		map[string]string{
			"SystemSettings": embedded,
		}, vsmsSelectors()...)
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
