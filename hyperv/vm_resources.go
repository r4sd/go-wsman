package hyperv

import (
	"context"
	"fmt"

	"github.com/r4sd/go-wsman/wsman"
)

const (
	msvmMemorySettingDataURI    = nsVirtV2 + "/Msvm_MemorySettingData"
	msvmProcessorSettingDataURI = nsVirtV2 + "/Msvm_ProcessorSettingData"
)

// settingDataInstanceIDPrefix は VM に紐づく SettingData の InstanceID プレフィックス。
//
// Hyper-V の慣習: SettingData の InstanceID は "Microsoft:<VM_GUID>\<RES_GUID>"。
// LIKE クエリで VM 単位の絞り込みに使う。
const settingDataInstanceIDPrefix = "Microsoft:"

// GetMemorySettings は指定 VM のメモリ設定 (Msvm_MemorySettingData) を取得する。
//
// vmName: 対象 VM の Msvm_ComputerSystem.Name (GUID)
//
// 1 VM につき 1 件だけ存在する想定。複数件返ってきた場合は先頭を返す。
func (c *Client) GetMemorySettings(ctx context.Context, vmName string) (*Msvm_MemorySettingData, error) {
	if vmName == "" {
		return nil, fmt.Errorf("GetMemorySettings: vmName must not be empty")
	}

	query := fmt.Sprintf(
		`SELECT * FROM Msvm_MemorySettingData WHERE InstanceID LIKE '%s%s%%'`,
		settingDataInstanceIDPrefix, vmName,
	)
	instances, err := c.wsman.Enumerate(ctx, msvmMemorySettingDataURI, wsman.WithWQL(query))
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("GetMemorySettings: not found for VM %q", vmName)
	}

	var m Msvm_MemorySettingData
	if err := Unmarshal(instances[0].Properties(), &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Msvm_MemorySettingData: %w", err)
	}
	return &m, nil
}

// SetMemorySettings はメモリ設定を変更する。
//
// 通常は GetMemorySettings → 値変更 → SetMemorySettings の流れ。
// settings.InstanceID で対象 VM のメモリ設定が一意に特定されるため、空は不可。
func (c *Client) SetMemorySettings(ctx context.Context, settings *Msvm_MemorySettingData) (string, error) {
	if settings == nil {
		return "", fmt.Errorf("SetMemorySettings: settings must not be nil")
	}
	if settings.InstanceID == "" {
		return "", fmt.Errorf("SetMemorySettings: settings.InstanceID must not be empty")
	}

	embedded, err := marshalEmbeddedInstance(settings, "Msvm_MemorySettingData", msvmMemorySettingDataURI)
	if err != nil {
		return "", fmt.Errorf("SetMemorySettings: marshal: %w", err)
	}

	result, err := c.ModifyResourceSettings(ctx, []string{embedded})
	if err != nil {
		return "", err
	}
	return result.JobRef, nil
}

// GetProcessorSettings は指定 VM の CPU 設定 (Msvm_ProcessorSettingData) を取得する。
func (c *Client) GetProcessorSettings(ctx context.Context, vmName string) (*Msvm_ProcessorSettingData, error) {
	if vmName == "" {
		return nil, fmt.Errorf("GetProcessorSettings: vmName must not be empty")
	}

	query := fmt.Sprintf(
		`SELECT * FROM Msvm_ProcessorSettingData WHERE InstanceID LIKE '%s%s%%'`,
		settingDataInstanceIDPrefix, vmName,
	)
	instances, err := c.wsman.Enumerate(ctx, msvmProcessorSettingDataURI, wsman.WithWQL(query))
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("GetProcessorSettings: not found for VM %q", vmName)
	}

	var p Msvm_ProcessorSettingData
	if err := Unmarshal(instances[0].Properties(), &p); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Msvm_ProcessorSettingData: %w", err)
	}
	return &p, nil
}

// SetProcessorSettings は CPU 設定を変更する。
func (c *Client) SetProcessorSettings(ctx context.Context, settings *Msvm_ProcessorSettingData) (string, error) {
	if settings == nil {
		return "", fmt.Errorf("SetProcessorSettings: settings must not be nil")
	}
	if settings.InstanceID == "" {
		return "", fmt.Errorf("SetProcessorSettings: settings.InstanceID must not be empty")
	}

	embedded, err := marshalEmbeddedInstance(settings, "Msvm_ProcessorSettingData", msvmProcessorSettingDataURI)
	if err != nil {
		return "", fmt.Errorf("SetProcessorSettings: marshal: %w", err)
	}

	result, err := c.ModifyResourceSettings(ctx, []string{embedded})
	if err != nil {
		return "", err
	}
	return result.JobRef, nil
}
