package hyperv

import (
	"context"
	"fmt"
	"strconv"

	"github.com/r4sd/go-wsman/wsman"
)

const (
	msvmImageManagementServiceURI    = nsVirtV2 + "/Msvm_ImageManagementService"
	msvmVirtualHardDiskSettingDataNS = nsVirtV2 + "/Msvm_VirtualHardDiskSettingData"
)

// imsSelectors は Msvm_ImageManagementService (シングルトンサービス) のメソッド呼び出しに付与する
// SelectorSet を返す。VSMS (vsmsSelectors) と同じく、Hyper-V WMI プロバイダ (WsmWmiPl.dll) は
// メソッド実行時に対象サービスインスタンスの特定を要求するため CreationClassName の Selector が要る。
// これを欠くと Get/Create/Resize VirtualHardDisk が InternalError になる (#89、実機で確認)。
func imsSelectors() []wsman.Selector {
	return []wsman.Selector{
		{Name: "CreationClassName", Value: "Msvm_ImageManagementService"},
	}
}

// GetVirtualHardDisk は指定パスの VHD/VHDX ファイルの設定情報を取得する。
//
// 内部では Msvm_ImageManagementService.GetVirtualHardDiskSettingData を呼び出し、
// 戻り値の SettingData (CIM EmbeddedInstance XML) をパースして返す。
func (c *Client) GetVirtualHardDisk(ctx context.Context, path string) (*Msvm_VirtualHardDiskSettingData, error) {
	resp, err := c.wsman.Invoke(ctx, msvmImageManagementServiceURI, "GetVirtualHardDiskSettingData",
		map[string]string{"Path": path}, imsSelectors()...)
	if err != nil {
		return nil, err
	}

	settingDataXML := resp.Property("SettingData")
	if settingDataXML == "" {
		return nil, fmt.Errorf("GetVirtualHardDisk: SettingData が空（path=%q）", path)
	}

	props, err := parseEmbeddedInstance(settingDataXML)
	if err != nil {
		return nil, fmt.Errorf("GetVirtualHardDisk: SettingData パース失敗: %w", err)
	}

	var settings Msvm_VirtualHardDiskSettingData
	if err := Unmarshal(props, &settings); err != nil {
		return nil, fmt.Errorf("GetVirtualHardDisk: Unmarshal 失敗: %w", err)
	}
	return &settings, nil
}

// CreateVirtualHardDisk は新規 VHD/VHDX ファイルを作成する。
//
// settings の必須フィールド:
//   - Path: 作成先のフルパス（例: "D:\\VMs\\new.vhdx"）
//   - MaxInternalSize: 論理サイズ（バイト単位）
//   - VirtualDiskFormat: VHDFormatVHD / VHDFormatVHDX 等
//   - VirtualDiskType: VHDTypeFixed / VHDTypeDynamic 等
//
// CIM の慣習でゼロ値フィールドは送信されない（デフォルト値が適用される）。
//
// 非同期 Job (Msvm_StorageJob) の完了を内部で待ってから返る (待機済みのため戻り値の
// Job 参照文字列は常に空)。待機のタイムアウト等は opts (WithJobTimeout 等) で調整できる。
// 大容量 Fixed VHD 作成が既定の 5 分を超える場合は WithJobTimeout を渡すこと。
func (c *Client) CreateVirtualHardDisk(ctx context.Context, settings *Msvm_VirtualHardDiskSettingData, opts ...WaitOption) (string, error) {
	if settings == nil {
		return "", fmt.Errorf("CreateVirtualHardDisk: settings must not be nil")
	}
	if settings.Path == "" {
		return "", fmt.Errorf("CreateVirtualHardDisk: settings.Path must not be empty")
	}

	embedded, err := marshalEmbeddedInstance(settings, "Msvm_VirtualHardDiskSettingData", msvmVirtualHardDiskSettingDataNS)
	if err != nil {
		return "", fmt.Errorf("CreateVirtualHardDisk: marshal 失敗: %w", err)
	}

	resp, err := c.wsman.Invoke(ctx, msvmImageManagementServiceURI, "CreateVirtualHardDisk",
		map[string]string{"VirtualDiskSettingData": embedded}, imsSelectors()...)
	if err != nil {
		return "", err
	}

	// ReturnValue: 0=同期成功, 4096=非同期 Job 開始
	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return "", fmt.Errorf("CreateVirtualHardDisk: unexpected ReturnValue=%s", rv)
	}
	if err := c.waitImageJob(ctx, resp, "CreateVirtualHardDisk", opts...); err != nil {
		return "", err
	}
	return "", nil
}

// waitImageJob は ImageManagementService の非同期メソッド (ReturnValue=4096) が返した Job の
// 完了を待つ。VHD 系 Job は Msvm_StorageJob (Msvm_ConcreteJob と別クラス) なので、EPR の
// ResourceURI を使う WaitForJobEPR で待つ。ReturnValue=0 (同期完了) は待つものなし。
// opts は待機のタイムアウト/ポーリング間隔を呼び出し側から調整するために透過する。
func (c *Client) waitImageJob(ctx context.Context, resp *wsman.InvokeResponse, opName string, opts ...WaitOption) error {
	if resp.ReturnValue != "4096" {
		return nil
	}
	epr, ok := resp.PropertyEPR("Job")
	if !ok {
		return fmt.Errorf("%s: ReturnValue=4096 だが Job EPR が取得できない", opName)
	}
	if err := c.WaitForJobEPR(ctx, epr, opts...); err != nil {
		return fmt.Errorf("%s: %w", opName, err)
	}
	return nil
}

// ResizeVirtualHardDisk は既存の VHD/VHDX ファイルのサイズを変更する。
//
// 内部では Msvm_ImageManagementService.ResizeVirtualHardDisk を呼び出す。
// Hyper-V の制約:
//   - Fixed VHD は拡大のみ可能（縮小不可）
//   - Dynamic/Differencing は MaxInternalSize の縮小も可（実データ末尾までに限る）
//   - VM へアタッチ中の VHD はオフライン状態のみ縮小可
//
// 非同期 Job (Msvm_StorageJob) の完了を内部で待ってから返る (戻り値の Job 参照文字列は常に空)。
// 待機のタイムアウト等は opts (WithJobTimeout 等) で調整できる。
func (c *Client) ResizeVirtualHardDisk(ctx context.Context, path string, maxInternalSize uint64, opts ...WaitOption) (string, error) {
	if path == "" {
		return "", fmt.Errorf("ResizeVirtualHardDisk: path must not be empty")
	}

	resp, err := c.wsman.Invoke(ctx, msvmImageManagementServiceURI, "ResizeVirtualHardDisk",
		map[string]string{
			"Path":            path,
			"MaxInternalSize": strconv.FormatUint(maxInternalSize, 10),
		}, imsSelectors()...)
	if err != nil {
		return "", err
	}

	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return "", fmt.Errorf("ResizeVirtualHardDisk: unexpected ReturnValue=%s", rv)
	}
	if err := c.waitImageJob(ctx, resp, "ResizeVirtualHardDisk", opts...); err != nil {
		return "", err
	}
	return "", nil
}
