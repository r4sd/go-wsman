package hyperv

import (
	"context"
	"fmt"
	"strconv"

	"github.com/r4sd/go-wsman/wsman"
)

const (
	msvmVirtualSystemSnapshotServiceURI = nsVirtV2 + "/Msvm_VirtualSystemSnapshotService"
)

// CreateSnapshot の SnapshotType 引数 (#57)。
// 一次資料: https://learn.microsoft.com/en-us/windows/win32/hyperv_v2/createsnapshot-msvm-virtualsystemsnapshotservice
const (
	SnapshotTypeFull uint16 = 2 // Complete snapshot of the virtual machine
	SnapshotTypeDisk uint16 = 3 // Snapshot of virtual machine disks
)

// vssSelectors は Msvm_VirtualSystemSnapshotService (シングルトン) のメソッド呼び出しに
// 付与する SelectorSet を返す。vsmsSelectors (vm.go) と同じ想定 (CreationClassName だけで
// シングルトンを一意特定できる) だが、こちらは実機 acc test で未確認 (#57 着手時点)。
// 実機で WBEM_E_INVALID_METHOD_PARAMETERS になる場合は selector を見直すこと。
func vssSelectors() []wsman.Selector {
	return []wsman.Selector{
		{Name: "CreationClassName", Value: "Msvm_VirtualSystemSnapshotService"},
	}
}

// matchSnapshotSettingDataForVM は SettingData が指定 VM のチェックポイント
// (VirtualSystemType=Snapshot:Realized) かを判定する純関数。
// matchRealizedSettingDataForVM (vm.go) の Snapshot 版。
func matchSnapshotSettingDataForVM(vmIdentifier, vmType, vmName string) bool {
	return vmIdentifier == vmName && vmType == VirtualSystemTypeSnapshotRealized
}

// CreateVmCheckpointResult は CreateVmCheckpoint の戻り値。
type CreateVmCheckpointResult struct {
	JobRef      string // 非同期 Job 参照 (Msvm_ConcreteJob.InstanceID)。同期成功時は空。
	SnapshotRef string // 作成されたチェックポイントの Msvm_VirtualSystemSettingData.InstanceID
	ReturnValue string // "0"=同期成功, "4096"=非同期 Job 開始
}

// CreateVmCheckpoint は VM のチェックポイントを作成する
// (Msvm_VirtualSystemSnapshotService.CreateSnapshot)。
//
// snapshotType は SnapshotTypeFull または SnapshotTypeDisk。SnapshotSettings は
// 一次資料上オプショナル (公式サンプルも空文字列を渡す) のため常に空文字列を送る。
func (c *Client) CreateVmCheckpoint(ctx context.Context, vmName string, snapshotType uint16) (*CreateVmCheckpointResult, error) {
	if vmName == "" {
		return nil, fmt.Errorf("CreateVmCheckpoint: vmName must not be empty")
	}

	affected := buildEndpointReference(msvmComputerSystemURI, map[string]string{
		"Name": vmName,
	})

	resp, err := c.wsman.Invoke(ctx, msvmVirtualSystemSnapshotServiceURI, "CreateSnapshot",
		map[string]string{
			"AffectedSystem":   affected,
			"SnapshotSettings": "",
			"SnapshotType":     strconv.FormatUint(uint64(snapshotType), 10),
		}, vssSelectors()...)
	if err != nil {
		return nil, err
	}

	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return nil, fmt.Errorf("CreateVmCheckpoint: unexpected ReturnValue=%s", rv)
	}

	result := &CreateVmCheckpointResult{
		JobRef:      resp.Property("Job"),
		SnapshotRef: resp.Property("ResultingSnapshot"),
		ReturnValue: rv,
	}
	if rv == "4096" && result.JobRef == "" {
		return nil, fmt.Errorf("CreateVmCheckpoint: ReturnValue=4096 but no Job reference")
	}
	return result, nil
}

// ApplyVmCheckpoint はチェックポイントを VM に適用(復元)する
// (Msvm_VirtualSystemSnapshotService.ApplySnapshot)。
//
// checkpointInstanceID は Msvm_VirtualSystemSettingData.InstanceID (チェックポイント自体のキー)。
// 一次資料上この操作は常に非同期 (ApplySnapshot は必ず Job=4096 を返す)。
func (c *Client) ApplyVmCheckpoint(ctx context.Context, checkpointInstanceID string) (string, error) {
	if checkpointInstanceID == "" {
		return "", fmt.Errorf("ApplyVmCheckpoint: checkpointInstanceID must not be empty")
	}

	snapshot := buildEndpointReference(msvmVirtualSystemSettingDataURI, map[string]string{
		"InstanceID": checkpointInstanceID,
	})

	resp, err := c.wsman.Invoke(ctx, msvmVirtualSystemSnapshotServiceURI, "ApplySnapshot",
		map[string]string{"Snapshot": snapshot}, vssSelectors()...)
	if err != nil {
		return "", err
	}

	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return "", fmt.Errorf("ApplyVmCheckpoint: unexpected ReturnValue=%s", rv)
	}

	jobRef := resp.Property("Job")
	if rv == "4096" && jobRef == "" {
		return "", fmt.Errorf("ApplyVmCheckpoint: ReturnValue=4096 but no Job reference")
	}
	return jobRef, nil
}

// DestroyVmCheckpoint はチェックポイントを削除する
// (Msvm_VirtualSystemSnapshotService.DestroySnapshot)。
//
// checkpointInstanceID は Msvm_VirtualSystemSettingData.InstanceID。仕様上、対象に依存する
// 子チェックポイントも副作用として削除されうる (一次資料の Description に明記)。
func (c *Client) DestroyVmCheckpoint(ctx context.Context, checkpointInstanceID string) (string, error) {
	if checkpointInstanceID == "" {
		return "", fmt.Errorf("DestroyVmCheckpoint: checkpointInstanceID must not be empty")
	}

	affected := buildEndpointReference(msvmVirtualSystemSettingDataURI, map[string]string{
		"InstanceID": checkpointInstanceID,
	})

	resp, err := c.wsman.Invoke(ctx, msvmVirtualSystemSnapshotServiceURI, "DestroySnapshot",
		map[string]string{"AffectedSnapshot": affected}, vssSelectors()...)
	if err != nil {
		return "", err
	}

	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return "", fmt.Errorf("DestroyVmCheckpoint: unexpected ReturnValue=%s", rv)
	}

	jobRef := resp.Property("Job")
	if rv == "4096" && jobRef == "" {
		return "", fmt.Errorf("DestroyVmCheckpoint: ReturnValue=4096 but no Job reference")
	}
	return jobRef, nil
}

// RenameVmCheckpoint はチェックポイントの表示名 (ElementName) を変更する (#123)。
//
// CreateSnapshot はチェックポイント名を指定する経路を持たない (SnapshotSettings の型
// Msvm_VirtualSystemSnapshotSettingData は ConsistencyLevel / IgnoreNonSnapshottableDisks /
// GuestBackupType の 3 つだけで ElementName を含まない)。Hyper-V は既定で
// "<VM名> - (YYYY/MM/DD - HH:MM:SS)" を付けるため、任意の名前にするには作成後の
// リネームが必要になる。
//
// 実装は VSMS の ModifySystemSettings (UpdateVm と同じ CIM メソッド)。スナップショットの
// SettingData (VirtualSystemType=Snapshot:Realized) を受理することは一次資料からは
// 判断できなかったため実機で確認済み (ElementName が実際に書き換わることを読み直して検証)。
func (c *Client) RenameVmCheckpoint(ctx context.Context, checkpointInstanceID, newName string) (string, error) {
	if checkpointInstanceID == "" {
		return "", fmt.Errorf("RenameVmCheckpoint: checkpointInstanceID must not be empty")
	}
	if newName == "" {
		// ElementName はゼロ値だと marshalEmbeddedInstance が送信しない (= 変更なし) ため、
		// 空名を「リネームした」と誤報告しないよう明示的に弾く。
		return "", fmt.Errorf("RenameVmCheckpoint: newName must not be empty")
	}

	return c.UpdateVm(ctx, &Msvm_VirtualSystemSettingData{
		InstanceID:  checkpointInstanceID,
		ElementName: newName,
	})
}

// ListVmCheckpoints は指定 VM のチェックポイント (Snapshot:Realized) を列挙する。
//
// GetSystemSettingData/ListSystemSettingData (vm.go) と同じパターン: Hyper-V は WQL
// フィルタ列挙を拒否するため (#80)、無フィルタ列挙 + Go 側フィルタで絞り込む。
func (c *Client) ListVmCheckpoints(ctx context.Context, vmName string) ([]*Msvm_VirtualSystemSettingData, error) {
	if vmName == "" {
		return nil, fmt.Errorf("ListVmCheckpoints: vmName must not be empty")
	}

	instances, err := c.enumerateFiltered(ctx, msvmVirtualSystemSettingDataURI, func(inst *wsman.Instance) bool {
		return matchSnapshotSettingDataForVM(inst.Property("VirtualSystemIdentifier"), inst.Property("VirtualSystemType"), vmName)
	})
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
