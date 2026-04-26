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
