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
// InstanceID は "Microsoft:GuestNetwork\<VM GUID>\<NIC 側 GUID>" 形式で VM GUID を含むが
// (実機確認済み、types.go 参照)、対応する Port SettingData の InstanceID とは "GuestNetwork\" の
// 有無だけが違い単純な文字列操作では対応が取りづらいため、この primitive は go-wsman 側で
// VM 絞り込みをしない。呼び出し側が ListSettingDataComponents の結果 (Port InstanceID → この
// InstanceID への対応) で目的の VM/NIC に絞り込む。
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
// GroupComponent/PartComponent は Parent/HostResource と違い WMI オブジェクトパス文字列ではなく
// 素の InstanceID がそのまま返る (実機確認済み、types.go 参照)。呼び出し側は Port/
// GuestNetworkAdapterConfiguration の InstanceID とそのまま突き合わせられる。
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
