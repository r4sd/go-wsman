package hyperv

import (
	"context"
	"fmt"

	"github.com/r4sd/go-wsman/wsman"
)

const (
	msvmVirtualSystemSettingDataURI = nsVirtV2 + "/Msvm_VirtualSystemSettingData"
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
	if err := Unmarshal(instances[0].Properties(), &settings); err != nil {
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
		if err := Unmarshal(inst.Properties(), &settings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_VirtualSystemSettingData: %w", err)
		}
		result = append(result, &settings)
	}
	return result, nil
}
