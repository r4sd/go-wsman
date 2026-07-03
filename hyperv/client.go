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

	// hostName は WMI オブジェクトパス (embedded の Parent/HostResource) の \\HOST\ 前置に使う
	// Hyper-V ホストのコンピュータ名。空の場合は相対パスを使う (前置を省く)。
	hostName string
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
	// 実機で動く無フィルタ列挙を使う。実機 acc test で確認。#80)
	instances, err := c.enumerateFiltered(ctx, msvmComputerSystemURI, func(inst *wsman.Instance) bool {
		return inst.Property("ElementName") == elementName
	})
	if err != nil {
		return nil, err
	}
	matches := make([]*Msvm_ComputerSystem, 0, len(instances))
	for _, inst := range instances {
		var cs Msvm_ComputerSystem
		if err := Unmarshal(inst.Properties(), &cs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_ComputerSystem: %w", err)
		}
		matches = append(matches, &cs)
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

// enumerateFiltered は resourceURI を無フィルタで Enumerate し、keep が true を返した
// インスタンスだけを返す共通ヘルパー。
//
// Hyper-V (root/virtualization/v2) のプロバイダは WS-Man 経由の WQL フィルタ列挙を
// CannotProcessFilter で拒否する (#80, omi#398 同型)。このため本パッケージの
// read/list/get 系は「サーバー側 WHERE 句」の代わりに「全件列挙 + クライアント側述語
// フィルタ」を共通利用する。元の WQL の WHERE 条件は keep 述語に移す。
//
// なお wsman.WithWQL API 自体は残してある (root/cimv2 等、WQL フィルタが効く namespace
// 向け)。Hyper-V namespace では使わないこと。
func (c *Client) enumerateFiltered(ctx context.Context, resourceURI string, keep func(*wsman.Instance) bool) ([]*wsman.Instance, error) {
	instances, err := c.wsman.Enumerate(ctx, resourceURI)
	if err != nil {
		return nil, err
	}
	matched := make([]*wsman.Instance, 0, len(instances))
	for _, inst := range instances {
		if keep(inst) {
			matched = append(matched, inst)
		}
	}
	return matched, nil
}

// matchSettingDataVM は SettingData の InstanceID が指定 VM (GUID) に属するかを判定する純関数。
//
// Hyper-V の SettingData InstanceID は "Microsoft:<VM_GUID>\<RES_GUID>" 形式
// (settingDataInstanceIDPrefix = "Microsoft:")。元の WQL
// `InstanceID LIKE 'Microsoft:<guid>%'` と同じ前方一致を行う。LIKE のパターンは
// prefix + ワイルドカードなので strings.HasPrefix と等価。
func matchSettingDataVM(instanceID, vmGUID string) bool {
	return strings.HasPrefix(instanceID, settingDataInstanceIDPrefix+vmGUID)
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
