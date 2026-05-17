package hyperv

import (
	"context"
	"fmt"
	"strconv"
)

const (
	msvmImageManagementServiceURI    = nsVirtV2 + "/Msvm_ImageManagementService"
	msvmVirtualHardDiskSettingDataNS = nsVirtV2 + "/Msvm_VirtualHardDiskSettingData"
)

// GetVirtualHardDisk は指定パスの VHD/VHDX ファイルの設定情報を取得する。
//
// 内部では Msvm_ImageManagementService.GetVirtualHardDiskSettingData を呼び出し、
// 戻り値の SettingData (CIM EmbeddedInstance XML) をパースして返す。
func (c *Client) GetVirtualHardDisk(ctx context.Context, path string) (*Msvm_VirtualHardDiskSettingData, error) {
	resp, err := c.wsman.Invoke(ctx, msvmImageManagementServiceURI, "GetVirtualHardDiskSettingData",
		map[string]string{"Path": path})
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
//   - Path: 作成先のフルパス（例: "C:\\VMs\\new.vhdx"）
//   - MaxInternalSize: 論理サイズ（バイト単位）
//   - VirtualDiskFormat: VHDFormatVHD / VHDFormatVHDX 等
//   - VirtualDiskType: VHDTypeFixed / VHDTypeDynamic 等
//
// CIM の慣習でゼロ値フィールドは送信されない（デフォルト値が適用される）。
//
// 戻り値は非同期 Job への参照（Msvm_ConcreteJob の InstanceID）。
// 0 件のリトリーバル要求でも 4096 (Method parameters checked - job started) が返る仕様。
func (c *Client) CreateVirtualHardDisk(ctx context.Context, settings *Msvm_VirtualHardDiskSettingData) (string, error) {
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
		map[string]string{"VirtualDiskSettingData": embedded})
	if err != nil {
		return "", err
	}

	// ReturnValue: 0=同期成功, 4096=非同期 Job 開始
	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return "", fmt.Errorf("CreateVirtualHardDisk: unexpected ReturnValue=%s", rv)
	}

	jobRef := resp.Property("Job")
	if jobRef == "" && rv == "4096" {
		return "", fmt.Errorf("CreateVirtualHardDisk: ReturnValue=4096 but no Job reference")
	}
	return jobRef, nil
}

// ResizeVirtualHardDisk は既存の VHD/VHDX ファイルのサイズを変更する。
//
// 内部では Msvm_ImageManagementService.ResizeVirtualHardDisk を呼び出す。
// Hyper-V の制約:
//   - Fixed VHD は拡大のみ可能（縮小不可）
//   - Dynamic/Differencing は MaxInternalSize の縮小も可（実データ末尾までに限る）
//   - VM へアタッチ中の VHD はオフライン状態のみ縮小可
//
// 戻り値は非同期 Job 参照 (Msvm_ConcreteJob)。
// ReturnValue=0 (同期完了) の場合は空文字列、4096 (非同期開始) の場合は Job 参照を返す。
func (c *Client) ResizeVirtualHardDisk(ctx context.Context, path string, maxInternalSize uint64) (string, error) {
	if path == "" {
		return "", fmt.Errorf("ResizeVirtualHardDisk: path must not be empty")
	}

	resp, err := c.wsman.Invoke(ctx, msvmImageManagementServiceURI, "ResizeVirtualHardDisk",
		map[string]string{
			"Path":            path,
			"MaxInternalSize": strconv.FormatUint(maxInternalSize, 10),
		})
	if err != nil {
		return "", err
	}

	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return "", fmt.Errorf("ResizeVirtualHardDisk: unexpected ReturnValue=%s", rv)
	}

	jobRef := resp.Property("Job")
	if rv == "4096" && jobRef == "" {
		return "", fmt.Errorf("ResizeVirtualHardDisk: ReturnValue=4096 but no Job reference")
	}
	return jobRef, nil
}
