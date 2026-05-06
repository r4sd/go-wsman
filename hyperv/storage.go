package hyperv

import (
	"context"
	"fmt"

	"github.com/r4sd/go-wsman/wsman"
)

const (
	msvmResourceAllocationSettingDataURI = nsVirtV2 + "/Msvm_ResourceAllocationSettingData"
	msvmStorageAllocationSettingDataURI  = nsVirtV2 + "/Msvm_StorageAllocationSettingData"
)

// ControllerType は VHD/DVD のアタッチ先 Controller 種別を表す。
type ControllerType string

const (
	ControllerTypeIDE  ControllerType = "IDE"
	ControllerTypeSCSI ControllerType = "SCSI" // Phase 4 では未対応 (将来用)
)

// AttachVHDOptions は AttachVHD のオプション。
type AttachVHDOptions struct {
	// ControllerType は接続先 Controller 種別。Phase 4 では IDE のみサポート。
	ControllerType ControllerType

	// ControllerNumber は IDE の場合 0 または 1 (VM はデフォルトで 2 個の IDE Controller を持つ)。
	ControllerNumber int

	// ControllerLocation は Controller 内の位置 (IDE の場合 0 または 1)。
	ControllerLocation int

	// Path は VHD/VHDX ファイルのフルパス (Hyper-V ホスト上のローカルパス)。
	Path string
}

// AttachDVDOptions は AttachDVD のオプション。
type AttachDVDOptions struct {
	ControllerType     ControllerType
	ControllerNumber   int
	ControllerLocation int
	Path               string // ISO ファイルのフルパス
}

// AttachResult はアタッチ操作の結果。
//
// DriveRef は作成された Drive (Msvm_ResourceAllocationSettingData) の参照、
// StorageRef は作成された Storage (Msvm_StorageAllocationSettingData) の参照。
// Detach 時は DriveRef を InstanceID として削除すれば Storage も連鎖削除される。
type AttachResult struct {
	DriveRef   string
	StorageRef string
	JobRef     string
}

// ListIDEControllers は VM の IDE Controller 一覧を返す。
//
// 各 VM は通常 2 つの IDE Controller (番号 0, 1) を持ち、それぞれ最大 2 つの
// Drive を接続できる (合計 4 ドライブ)。AttachVHD のターゲット指定に使う。
func (c *Client) ListIDEControllers(ctx context.Context, vmName string) ([]*Msvm_ResourceAllocationSettingData, error) {
	if vmName == "" {
		return nil, fmt.Errorf("ListIDEControllers: vmName must not be empty")
	}

	query := fmt.Sprintf(
		`SELECT * FROM Msvm_ResourceAllocationSettingData WHERE InstanceID LIKE '%s%s%%' AND ResourceSubType="%s"`,
		settingDataInstanceIDPrefix, vmName, ResourceSubTypeIDEController,
	)
	instances, err := c.wsman.Enumerate(ctx, msvmResourceAllocationSettingDataURI, wsman.WithWQL(query))
	if err != nil {
		return nil, err
	}
	result := make([]*Msvm_ResourceAllocationSettingData, 0, len(instances))
	for _, inst := range instances {
		var r Msvm_ResourceAllocationSettingData
		if err := Unmarshal(inst.Properties(), &r); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_ResourceAllocationSettingData: %w", err)
		}
		result = append(result, &r)
	}
	return result, nil
}

// ListAttachedStorage は VM にアタッチされた VHD/ISO ファイルの一覧を返す。
//
// terraform の差分計算や、アタッチ済みディスクの確認に使う。
func (c *Client) ListAttachedStorage(ctx context.Context, vmName string) ([]*Msvm_StorageAllocationSettingData, error) {
	if vmName == "" {
		return nil, fmt.Errorf("ListAttachedStorage: vmName must not be empty")
	}

	query := fmt.Sprintf(
		`SELECT * FROM Msvm_StorageAllocationSettingData WHERE InstanceID LIKE '%s%s%%'`,
		settingDataInstanceIDPrefix, vmName,
	)
	instances, err := c.wsman.Enumerate(ctx, msvmStorageAllocationSettingDataURI, wsman.WithWQL(query))
	if err != nil {
		return nil, err
	}
	result := make([]*Msvm_StorageAllocationSettingData, 0, len(instances))
	for _, inst := range instances {
		var s Msvm_StorageAllocationSettingData
		if err := Unmarshal(inst.Properties(), &s); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_StorageAllocationSettingData: %w", err)
		}
		result = append(result, &s)
	}
	return result, nil
}

// AttachVHD は VHD/VHDX ファイルを VM にアタッチする。
//
// 内部で 2 段階の AddResourceSettings を実行する:
//  1. Msvm_ResourceAllocationSettingData (Disk Drive) を Controller に追加
//  2. Msvm_StorageAllocationSettingData (VHD ファイル) を Drive に紐付け
//
// 1 が成功して 2 が失敗した場合、空の Drive が残る (実害は少ないが手動削除推奨)。
//
// Phase 4 では ControllerType=IDE のみサポート。SCSI は将来対応。
func (c *Client) AttachVHD(ctx context.Context, vmName string, opts AttachVHDOptions) (*AttachResult, error) {
	return c.attachStorage(ctx, vmName, attachOpts{
		ControllerType:     opts.ControllerType,
		ControllerNumber:   opts.ControllerNumber,
		ControllerLocation: opts.ControllerLocation,
		Path:               opts.Path,
		DriveSubType:       ResourceSubTypeSyntheticDiskDrive,
		StorageSubType:     ResourceSubTypeVirtualHardDisk,
		StorageResType:     ResourceTypeStorageExtent,
		DriveResType:       ResourceTypeDiskDrive,
		opName:             "AttachVHD",
	})
}

// AttachDVD は ISO ファイルを VM の DVD ドライブとしてマウントする。
func (c *Client) AttachDVD(ctx context.Context, vmName string, opts AttachDVDOptions) (*AttachResult, error) {
	return c.attachStorage(ctx, vmName, attachOpts{
		ControllerType:     opts.ControllerType,
		ControllerNumber:   opts.ControllerNumber,
		ControllerLocation: opts.ControllerLocation,
		Path:               opts.Path,
		DriveSubType:       ResourceSubTypeSyntheticDVDDrive,
		StorageSubType:     ResourceSubTypeVirtualCDDVDDisk,
		StorageResType:     ResourceTypeStorageExtent,
		DriveResType:       ResourceTypeDVDDrive,
		opName:             "AttachDVD",
	})
}

// attachOpts は VHD/DVD アタッチの内部共通パラメータ。
type attachOpts struct {
	ControllerType     ControllerType
	ControllerNumber   int
	ControllerLocation int
	Path               string
	DriveSubType       string
	StorageSubType     string
	DriveResType       uint16
	StorageResType     uint16
	opName             string // エラーメッセージ用
}

// attachStorage は VHD/DVD アタッチの共通実装。
func (c *Client) attachStorage(ctx context.Context, vmName string, opts attachOpts) (*AttachResult, error) {
	if vmName == "" {
		return nil, fmt.Errorf("%s: vmName must not be empty", opts.opName)
	}
	if opts.Path == "" {
		return nil, fmt.Errorf("%s: Path must not be empty", opts.opName)
	}
	if opts.ControllerType != ControllerTypeIDE {
		return nil, fmt.Errorf("%s: only IDE controller is supported in Phase 4 (got %q)", opts.opName, opts.ControllerType)
	}

	// 1. ターゲットの IDE Controller を特定
	controllers, err := c.ListIDEControllers(ctx, vmName)
	if err != nil {
		return nil, fmt.Errorf("%s: list controllers: %w", opts.opName, err)
	}
	if opts.ControllerNumber >= len(controllers) {
		return nil, fmt.Errorf("%s: ControllerNumber %d out of range (VM has %d IDE controllers)",
			opts.opName, opts.ControllerNumber, len(controllers))
	}
	controller := controllers[opts.ControllerNumber]
	controllerEPR := buildEndpointReference(msvmResourceAllocationSettingDataURI, map[string]string{
		"InstanceID": controller.InstanceID,
	})

	// 2. Drive を Controller に追加
	drive := &Msvm_ResourceAllocationSettingData{
		ResourceType:    opts.DriveResType,
		ResourceSubType: opts.DriveSubType,
		Parent:          controllerEPR,
		AddressOnParent: fmt.Sprintf("%d", opts.ControllerLocation),
	}
	driveXML, err := marshalEmbeddedInstance(drive, "Msvm_ResourceAllocationSettingData", msvmResourceAllocationSettingDataURI)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal drive: %w", opts.opName, err)
	}
	driveResult, err := c.AddResourceSettings(ctx, vmName, []string{driveXML})
	if err != nil {
		return nil, fmt.Errorf("%s: add drive: %w", opts.opName, err)
	}
	result := &AttachResult{
		DriveRef: driveResult.ResultingResourceSettings,
		JobRef:   driveResult.JobRef,
	}

	// 3. ファイル (VHD/ISO) を Drive に紐付け
	driveEPR := buildEndpointReference(msvmResourceAllocationSettingDataURI, map[string]string{
		"InstanceID": result.DriveRef,
	})
	storage := &Msvm_StorageAllocationSettingData{
		ResourceType:    opts.StorageResType,
		ResourceSubType: opts.StorageSubType,
		HostResource:    opts.Path,
		Parent:          driveEPR,
	}
	storageXML, err := marshalEmbeddedInstance(storage, "Msvm_StorageAllocationSettingData", msvmStorageAllocationSettingDataURI)
	if err != nil {
		return result, fmt.Errorf("%s: marshal storage: %w", opts.opName, err)
	}
	storageResult, err := c.AddResourceSettings(ctx, vmName, []string{storageXML})
	if err != nil {
		return result, fmt.Errorf("%s: add storage: %w", opts.opName, err)
	}

	result.StorageRef = storageResult.ResultingResourceSettings
	result.JobRef = storageResult.JobRef
	return result, nil
}

// DetachStorage は Drive を VM から取り外す (VHD/DVD 共通)。
//
// driveInstanceID は Msvm_ResourceAllocationSettingData (Disk Drive or DVD Drive) の InstanceID。
// Drive を削除すると、その Drive に紐づく Storage (Msvm_StorageAllocationSettingData) も
// Hyper-V 側で連鎖削除される。
func (c *Client) DetachStorage(ctx context.Context, driveInstanceID string) (string, error) {
	if driveInstanceID == "" {
		return "", fmt.Errorf("DetachStorage: driveInstanceID must not be empty")
	}
	epr := buildEndpointReference(msvmResourceAllocationSettingDataURI, map[string]string{
		"InstanceID": driveInstanceID,
	})
	return c.RemoveResourceSettings(ctx, []string{epr})
}
