package hyperv

import (
	"context"
	"fmt"
	"sort"

	"github.com/r4sd/go-wsman/wsman"
)

// sortRASDByInstanceID は Controller/Drive 一覧を InstanceID で安定ソートする。
// WS-Man 列挙順は無保証なため、ControllerNumber を決定的にするのに使う。
func sortRASDByInstanceID(items []*Msvm_ResourceAllocationSettingData) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].InstanceID < items[j].InstanceID
	})
}

const (
	msvmResourceAllocationSettingDataURI = nsVirtV2 + "/Msvm_ResourceAllocationSettingData"
	msvmStorageAllocationSettingDataURI  = nsVirtV2 + "/Msvm_StorageAllocationSettingData"
)

// ControllerType は VHD/DVD のアタッチ先 Controller 種別を表す。
type ControllerType string

const (
	ControllerTypeIDE  ControllerType = "IDE"
	ControllerTypeSCSI ControllerType = "SCSI"
)

// Controller 内の location (AddressOnParent) の上限。
// IDE は 1 Controller あたり 2 ドライブ (0-1)、SCSI は 64 ドライブ (0-63)。
const (
	maxIDELocation  = 1
	maxSCSILocation = 63
)

// AttachVHDOptions は AttachVHD のオプション。
type AttachVHDOptions struct {
	// ControllerType は接続先 Controller 種別 (IDE または SCSI)。
	ControllerType ControllerType

	// ControllerNumber は同種 Controller の何番目か (IDE は通常 0 または 1)。
	ControllerNumber int

	// ControllerLocation は Controller 内の位置 (AddressOnParent)。IDE は 0-1、SCSI は 0-63。
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
// Detach は「Storage (SASD) を先に削除 → Drive (RASD) を削除」の2段が必須
// (子→親の逆順)。Drive 単独削除では子 SASD が残っているため VMMS が拒否する
// (0x80041001)。連鎖削除は起きない。DetachStorage を参照。
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

	// 元の WQL `InstanceID LIKE 'Microsoft:<guid>%' AND ResourceSubType="<IDE>"` を
	// Go 側フィルタに移す (Hyper-V は WQL フィルタ列挙を拒否する #80)。
	instances, err := c.enumerateFiltered(ctx, msvmResourceAllocationSettingDataURI, func(inst *wsman.Instance) bool {
		return matchSettingDataVM(inst.Property("InstanceID"), vmName) &&
			inst.Property("ResourceSubType") == ResourceSubTypeIDEController
	})
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

// ListSCSIControllers は VM の SCSI Controller 一覧を返す。
//
// Gen2 VM は IDE を持たず、ブートディスクは SCSI に接続する。各 SCSI Controller は最大
// 64 ドライブ (AddressOnParent 0-63) を接続できる。
//
// 注意: go-wsman の DefineSystem はシェル VM を作るため、Hyper-V の New-VM と違い Gen2 でも
// SCSI Controller が自動生成されない (#88、実機確認済)。go-wsman で作った VM に SCSI ブート
// ディスクを付ける場合は、先に AddScsiController で Controller を追加する必要がある。
func (c *Client) ListSCSIControllers(ctx context.Context, vmName string) ([]*Msvm_ResourceAllocationSettingData, error) {
	if vmName == "" {
		return nil, fmt.Errorf("ListSCSIControllers: vmName must not be empty")
	}

	// ListIDEControllers と同じく、WQL フィルタ列挙は Hyper-V が拒否する (#80) ため
	// 無フィルタ列挙 + Go 側フィルタ (対象 VM かつ Synthetic SCSI Controller) で絞る。
	instances, err := c.enumerateFiltered(ctx, msvmResourceAllocationSettingDataURI, func(inst *wsman.Instance) bool {
		return matchSettingDataVM(inst.Property("InstanceID"), vmName) &&
			inst.Property("ResourceSubType") == ResourceSubTypeSCSIController
	})
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

// AddControllerResult は Controller 追加操作の結果。
type AddControllerResult struct {
	ControllerRef string // 作成された Controller (Msvm_ResourceAllocationSettingData) の InstanceID
	JobRef        string
}

// AddScsiController は VM に Synthetic SCSI Controller を 1 つ追加する。
//
// go-wsman の DefineSystem はシェル VM を作り Gen2 でも SCSI Controller を持たない (#88) ため、
// SCSI ブートディスクを付ける前に本メソッドで Controller を追加する。追加した Controller は
// ListSCSIControllers に現れ、AttachVHD(ControllerType=SCSI) のターゲットになる。
func (c *Client) AddScsiController(ctx context.Context, vmName string) (*AddControllerResult, error) {
	if vmName == "" {
		return nil, fmt.Errorf("AddScsiController: vmName must not be empty")
	}
	controller := &Msvm_ResourceAllocationSettingData{
		ResourceType:    ResourceTypeParallelSCSI,
		ResourceSubType: ResourceSubTypeSCSIController,
	}
	controllerXML, err := marshalEmbeddedInstance(controller, "Msvm_ResourceAllocationSettingData", msvmResourceAllocationSettingDataURI)
	if err != nil {
		return nil, fmt.Errorf("AddScsiController: marshal: %w", err)
	}
	result, err := c.AddResourceSettings(ctx, vmName, []string{controllerXML})
	if err != nil {
		return nil, fmt.Errorf("AddScsiController: %w", err)
	}
	return &AddControllerResult{
		ControllerRef: result.ResultingResourceSettings,
		JobRef:        result.JobRef,
	}, nil
}

// ListDiskDrives は VM の Disk Drive (Msvm_ResourceAllocationSettingData, ResourceType=17)
// 一覧を返す。
//
// Drive は Parent に親 Controller (IDE/SCSI) の EPR、AddressOnParent に Controller 内の
// location を持つ。VHD の逆引き (どの Controller のどの位置に何の VHD が付いているか) は
// ListAttachedStorage(ファイル) の Parent が指す Drive を本メソッドの結果と突き合わせて行う。
func (c *Client) ListDiskDrives(ctx context.Context, vmName string) ([]*Msvm_ResourceAllocationSettingData, error) {
	if vmName == "" {
		return nil, fmt.Errorf("ListDiskDrives: vmName must not be empty")
	}

	// Controller 列挙と同じく WQL フィルタは Hyper-V が拒否する (#80) ため、無フィルタ列挙 +
	// Go 側フィルタ (対象 VM かつ Synthetic Disk Drive) で絞る。
	instances, err := c.enumerateFiltered(ctx, msvmResourceAllocationSettingDataURI, func(inst *wsman.Instance) bool {
		return matchSettingDataVM(inst.Property("InstanceID"), vmName) &&
			inst.Property("ResourceSubType") == ResourceSubTypeSyntheticDiskDrive
	})
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

// ListDvdDrives は VM の DVD Drive (Msvm_ResourceAllocationSettingData,
// ResourceSubType="Synthetic DVD Drive") の一覧を返す。
//
// ListDiskDrives が Disk Drive を返すのと対で、DVD の逆引き (storage→drive→controller) に使う。
// Gen2 は DVD も SCSI Controller に、Gen1 は IDE に接続する。
func (c *Client) ListDvdDrives(ctx context.Context, vmName string) ([]*Msvm_ResourceAllocationSettingData, error) {
	if vmName == "" {
		return nil, fmt.Errorf("ListDvdDrives: vmName must not be empty")
	}

	// Controller/Disk 列挙と同じく WQL フィルタは Hyper-V が拒否する (#80) ため、無フィルタ列挙 +
	// Go 側フィルタ (対象 VM かつ Synthetic DVD Drive) で絞る。
	instances, err := c.enumerateFiltered(ctx, msvmResourceAllocationSettingDataURI, func(inst *wsman.Instance) bool {
		return matchSettingDataVM(inst.Property("InstanceID"), vmName) &&
			inst.Property("ResourceSubType") == ResourceSubTypeSyntheticDVDDrive
	})
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

	// 元の WQL `InstanceID LIKE 'Microsoft:<guid>%'` を Go 側フィルタに移す
	// (Hyper-V は WQL フィルタ列挙を拒否する #80)。
	instances, err := c.enumerateFiltered(ctx, msvmStorageAllocationSettingDataURI, func(inst *wsman.Instance) bool {
		return matchSettingDataVM(inst.Property("InstanceID"), vmName)
	})
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
// ControllerType は IDE / SCSI をサポート。Gen2 VM のブートディスクは SCSI を使う。
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

// storageAllocationInput は AddResourceSettings で Msvm_StorageAllocationSettingData を送る際の
// 入力表現。HostResource は CIM 上 string[] なので配列で送る (読み取り用の
// Msvm_StorageAllocationSettingData は単一値でよいため別 struct にする)。
type storageAllocationInput struct {
	ResourceType    uint16   `cim:"ResourceType"`
	ResourceSubType string   `cim:"ResourceSubType"`
	HostResource    []string `cim:"HostResource"`
	Parent          string   `cim:"Parent"`
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

	// ControllerType ごとに location 上限とターゲット列挙方法を切り替える。
	var maxLocation int
	var listControllers func(context.Context, string) ([]*Msvm_ResourceAllocationSettingData, error)
	switch opts.ControllerType {
	case ControllerTypeIDE:
		maxLocation = maxIDELocation
		listControllers = c.ListIDEControllers
	case ControllerTypeSCSI:
		maxLocation = maxSCSILocation
		listControllers = c.ListSCSIControllers
	default:
		return nil, fmt.Errorf("%s: unsupported controller type %q (want IDE or SCSI)", opts.opName, opts.ControllerType)
	}
	if opts.ControllerLocation < 0 || opts.ControllerLocation > maxLocation {
		return nil, fmt.Errorf("%s: ControllerLocation %d out of range for %s (0-%d)",
			opts.opName, opts.ControllerLocation, opts.ControllerType, maxLocation)
	}

	// 1. ターゲットの Controller を特定
	controllers, err := listControllers(ctx, vmName)
	if err != nil {
		return nil, fmt.Errorf("%s: list controllers: %w", opts.opName, err)
	}
	// WS-Man の列挙順は無保証。ControllerNumber を安定させるため InstanceID でソートしてから
	// index で選ぶ (読み取り側 provider の mapHardDiskDriveRefs と同じ決定的順序に揃える)。
	sortRASDByInstanceID(controllers)
	if opts.ControllerNumber < 0 || opts.ControllerNumber >= len(controllers) {
		return nil, fmt.Errorf("%s: ControllerNumber %d out of range (VM has %d %s controllers)",
			opts.opName, opts.ControllerNumber, len(controllers), opts.ControllerType)
	}
	controller := controllers[opts.ControllerNumber]
	// Parent は CIM string プロパティ = WMI オブジェクトパス (WS-Addressing EPR ではない)。
	controllerPath := wmiObjectPath(c.hostName, msvmResourceAllocationSettingDataURI, map[string]string{
		"InstanceID": controller.InstanceID,
	})

	// 2. Drive を Controller に追加
	drive := &Msvm_ResourceAllocationSettingData{
		ResourceType:    opts.DriveResType,
		ResourceSubType: opts.DriveSubType,
		Parent:          controllerPath,
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
	// Drive 追加が非同期 Job の場合、完了を待ってから Storage を紐付ける。待たずに次の
	// AddResourceSettings で未実体化の Drive を Parent 参照すると、Hyper-V が
	// 「リソースを追加できませんでした」(Exception, ErrorCode=32773) で失敗する。
	if err := c.WaitForJob(ctx, driveResult.JobRef); err != nil {
		return result, fmt.Errorf("%s: wait add drive: %w", opts.opName, err)
	}

	// 3. ファイル (VHD/ISO) を Drive に紐付け
	drivePath := wmiObjectPath(c.hostName, msvmResourceAllocationSettingDataURI, map[string]string{
		"InstanceID": result.DriveRef,
	})
	// HostResource は CIM 上 string[] (配列) なので、AddResourceSettings では PROPERTY.ARRAY で
	// 送る必要がある。読み取り用 struct (HostResource string) と別に、配列表現の入力 struct を使う。
	storage := &storageAllocationInput{
		ResourceType:    opts.StorageResType,
		ResourceSubType: opts.StorageSubType,
		HostResource:    []string{opts.Path},
		Parent:          drivePath,
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
	// Storage 追加 Job の完了も待ち、AttachVHD/AttachDVD 返却時にアタッチが完了しているようにする。
	if err := c.WaitForJob(ctx, storageResult.JobRef); err != nil {
		return result, fmt.Errorf("%s: wait add storage: %w", opts.opName, err)
	}
	return result, nil
}

// DetachStorage は VHD/DVD をアタッチした Drive を VM から取り外す。
//
// storageInstanceID は Drive に紐づく Storage (Msvm_StorageAllocationSettingData)
// の InstanceID。空文字なら Storage 削除をスキップし Drive 単独を削除する
// (アタッチ失敗時に生じた VHD 未接続の空 Drive を掃除する rollback 用途)。
//
// 削除順は「Storage (SASD) → Drive (RASD)」の2段・別呼び出しが必須。子 SASD が
// 付いたまま Drive RASD を削除すると VMMS が 0x80041001 で拒否する (attach の逆順)。
// os-win (OpenStack Hyper-V) の detach_vm_disk も同順・別呼び出しで実装しており、
// 配列一括 (RemoveResourceSettings に両 EPR) は配列内処理順が無保証のため使わない。
// 各段の非同期 Job (4096) は完了を待つ (待たないと後続 Get がディスクを見せ続ける)。
//
// 戻り値は Drive 削除 Job の参照。
func (c *Client) DetachStorage(ctx context.Context, driveInstanceID, storageInstanceID string) (string, error) {
	if driveInstanceID == "" {
		return "", fmt.Errorf("DetachStorage: driveInstanceID must not be empty")
	}

	// ① Storage (SASD) を先に削除。EPR の ResourceURI は Msvm_StorageAllocationSettingData
	// (RASD の URI を使うと解決に失敗する)。
	if storageInstanceID != "" {
		storageEPR := buildEndpointReference(msvmStorageAllocationSettingDataURI, map[string]string{
			"InstanceID": storageInstanceID,
		})
		storageJob, err := c.RemoveResourceSettings(ctx, []string{storageEPR})
		if err != nil {
			return "", fmt.Errorf("DetachStorage: remove storage: %w", err)
		}
		if err := c.WaitForJob(ctx, storageJob); err != nil {
			return storageJob, fmt.Errorf("DetachStorage: wait storage: %w", err)
		}
	}

	// ② Drive (RASD) を削除。
	driveEPR := buildEndpointReference(msvmResourceAllocationSettingDataURI, map[string]string{
		"InstanceID": driveInstanceID,
	})
	jobRef, err := c.RemoveResourceSettings(ctx, []string{driveEPR})
	if err != nil {
		return "", fmt.Errorf("DetachStorage: remove drive: %w", err)
	}
	if err := c.WaitForJob(ctx, jobRef); err != nil {
		return jobRef, fmt.Errorf("DetachStorage: wait drive: %w", err)
	}
	return jobRef, nil
}
