package hyperv

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/r4sd/go-wsman/wsman"
)

// ErrVMNotFound は ElementName / GUID に一致する VM が存在しないことを表す sentinel error。
// provider 側で errors.Is により「不在」と「通信失敗」を区別するために使う。
var ErrVMNotFound = errors.New("hyperv: virtual machine not found")

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

// FindComputerSystemByElementName は表示名 (ElementName) から VM を取得する。
// 戻り値の Msvm_ComputerSystem.Name が GUID で、CIM の各操作はこの GUID を要求する。
//
// 複数一致は曖昧としてエラー、不在は ErrVMNotFound を返す。
// elementName は大小文字まで一致させること: WQL の比較は大小文字を区別しないが、
// クライアント側の最終照合は区別するため、ケース違いは ErrVMNotFound になる。
func (c *Client) FindComputerSystemByElementName(ctx context.Context, elementName string) (*Msvm_ComputerSystem, error) {
	if elementName == "" {
		return nil, fmt.Errorf("FindComputerSystemByElementName: elementName must not be empty")
	}
	// 全 Msvm_ComputerSystem を列挙して ElementName でクライアント側フィルタする。
	// (MS WS-Man Hyper-V は WQL フィルタ列挙を CannotProcessFilter で拒否するため、
	// 実機で動く無フィルタ列挙を使う。実機 acc test で確認。)
	instances, err := c.wsman.Enumerate(ctx, msvmComputerSystemURI)
	if err != nil {
		return nil, err
	}
	var matches []*Msvm_ComputerSystem
	for _, inst := range instances {
		var cs Msvm_ComputerSystem
		if err := Unmarshal(inst.Properties(), &cs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_ComputerSystem: %w", err)
		}
		if cs.ElementName == elementName {
			matches = append(matches, &cs)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("FindComputerSystemByElementName %q: %w", elementName, ErrVMNotFound)
	case 1:
		return matches[0], nil
	default:
		// Hyper-V は同名 VM を許す。黙って1件返すと誤った VM を操作しうるためエラーにする。
		return nil, fmt.Errorf("FindComputerSystemByElementName: %d VMs found with ElementName %q; name is ambiguous", len(matches), elementName)
	}
}

// wqlEscapeLiteral は WQL の二重引用符文字列リテラルに安全に埋め込めるよう、
// バックスラッシュと二重引用符をエスケープする (\ → \\, " → \")。
func wqlEscapeLiteral(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
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
