package hyperv

import (
	"context"
	"fmt"

	"github.com/r4sd/go-wsman/wsman"
)

const (
	nsVirtV2              = "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/virtualization/v2"
	msvmComputerSystemURI = nsVirtV2 + "/Msvm_ComputerSystem"
)

// Client は Hyper-V CIM クラスへの型付きアクセスを提供する。
type Client struct {
	wsman *wsman.Client
}

// NewClient は wsman.Client をラップした hyperv.Client を生成する。
func NewClient(endpoint string, opts ...wsman.ClientOption) (*Client, error) {
	wc, err := wsman.NewClient(endpoint, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{wsman: wc}, nil
}

// GetComputerSystem は Name（VM GUID）で単一 VM を取得する。
func (c *Client) GetComputerSystem(ctx context.Context, name string) (*Msvm_ComputerSystem, error) {
	resp, err := c.wsman.Get(ctx, msvmComputerSystemURI,
		wsman.Selector{Name: "Name", Value: name},
	)
	if err != nil {
		return nil, err
	}
	var cs Msvm_ComputerSystem
	if err := Unmarshal(resp.Properties(), &cs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Msvm_ComputerSystem: %w", err)
	}
	return &cs, nil
}

// FindComputerSystemByElementName は表示名 (ElementName) から単一 VM を取得する。
//
// CIM の各操作 (GetSystemSettingData / DestroySystem / RequestStateChange 等) は
// VM GUID (Msvm_ComputerSystem.Name) を要求するが、terraform-provider は VM 表示名で
// 操作する。本メソッドは「表示名→GUID 解決」の入口で、取得した Msvm_ComputerSystem.Name
// が GUID となる。
//
// ElementName を WQL でサーバー側フィルタした上で、クライアント側でも完全一致を確認する
// (WQL の大小文字・部分一致のクセに依存しない)。Hyper-V では表示名の一意性は保証されない
// が、homelab 運用では一意前提。複数一致時は最初の完全一致を返す。
// 該当が無い場合はエラーを返す。
func (c *Client) FindComputerSystemByElementName(ctx context.Context, elementName string) (*Msvm_ComputerSystem, error) {
	if elementName == "" {
		return nil, fmt.Errorf("FindComputerSystemByElementName: elementName must not be empty")
	}
	query := fmt.Sprintf(`SELECT * FROM Msvm_ComputerSystem WHERE ElementName="%s"`, elementName)
	instances, err := c.wsman.Enumerate(ctx, msvmComputerSystemURI, wsman.WithWQL(query))
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		var cs Msvm_ComputerSystem
		if err := Unmarshal(inst.Properties(), &cs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_ComputerSystem: %w", err)
		}
		if cs.ElementName == elementName {
			return &cs, nil
		}
	}
	return nil, fmt.Errorf("FindComputerSystemByElementName: no VM found with ElementName %q", elementName)
}

// ListComputerSystems は全 VM を Enumerate で取得する。
func (c *Client) ListComputerSystems(ctx context.Context) ([]*Msvm_ComputerSystem, error) {
	instances, err := c.wsman.Enumerate(ctx, msvmComputerSystemURI)
	if err != nil {
		return nil, err
	}
	result := make([]*Msvm_ComputerSystem, 0, len(instances))
	for _, inst := range instances {
		var cs Msvm_ComputerSystem
		if err := Unmarshal(inst.Properties(), &cs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_ComputerSystem: %w", err)
		}
		result = append(result, &cs)
	}
	return result, nil
}
