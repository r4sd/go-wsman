package hyperv

import (
	"context"
	"fmt"
)

const (
	msvmGuestNetworkAdapterConfigurationURI = nsVirtV2 + "/Msvm_GuestNetworkAdapterConfiguration"
	msvmSettingDataComponentURI             = nsVirtV2 + "/Msvm_SettingDataComponent"
)

// ListGuestNetworkAdapterConfigurations は全 VM のゲスト OS 内 NIC 設定 (IP アドレス等) を
// 無フィルタで列挙する。
//
// InstanceID がゲスト内 NIC 由来で VM GUID を含まない (実機確認要、types.go 参照) ため、この
// primitive は go-wsman 側で VM 絞り込みをしない。呼び出し側が ListSettingDataComponents の
// 結果 (Port InstanceID → この InstanceID への対応) で目的の VM/NIC に絞り込む。
func (c *Client) ListGuestNetworkAdapterConfigurations(ctx context.Context) ([]*Msvm_GuestNetworkAdapterConfiguration, error) {
	instances, err := c.wsman.Enumerate(ctx, msvmGuestNetworkAdapterConfigurationURI)
	if err != nil {
		return nil, err
	}
	result := make([]*Msvm_GuestNetworkAdapterConfiguration, 0, len(instances))
	for _, inst := range instances {
		var g Msvm_GuestNetworkAdapterConfiguration
		if err := UnmarshalList(inst.PropertiesList(), &g); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_GuestNetworkAdapterConfiguration: %w", err)
		}
		result = append(result, &g)
	}
	return result, nil
}

// ListSettingDataComponents は Port SettingData (Msvm_SyntheticEthernetPortSettingData 等) と
// Msvm_GuestNetworkAdapterConfiguration を結ぶ association を無フィルタで列挙する。
//
// GroupComponent/PartComponent の EPR 文字列から対象 InstanceID を取り出すのは呼び出し側の責務
// (Msvm_EthernetPortAllocationSettingData.Parent/HostResource と同じ抽出ロジックを使う)。
func (c *Client) ListSettingDataComponents(ctx context.Context) ([]*Msvm_SettingDataComponent, error) {
	instances, err := c.wsman.Enumerate(ctx, msvmSettingDataComponentURI)
	if err != nil {
		return nil, err
	}
	result := make([]*Msvm_SettingDataComponent, 0, len(instances))
	for _, inst := range instances {
		var s Msvm_SettingDataComponent
		if err := Unmarshal(inst.Properties(), &s); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_SettingDataComponent: %w", err)
		}
		result = append(result, &s)
	}
	return result, nil
}
