package hyperv

// EnabledState 定数（CIM 仕様）
const (
	EnabledStateUnknown  uint16 = 0
	EnabledStateEnabled  uint16 = 2     // Running
	EnabledStateDisabled uint16 = 3     // Off
	EnabledStatePaused   uint16 = 32768 // Paused
	EnabledStateSaved    uint16 = 32769 // Saved
)

// Msvm_ComputerSystem は Hyper-V VM を表す CIM クラス。
// 名前空間: root/virtualization/v2
type Msvm_ComputerSystem struct {
	Name                 string `cim:"Name"`        // VM GUID
	ElementName          string `cim:"ElementName"` // VM 表示名
	Caption              string `cim:"Caption"`
	Description          string `cim:"Description"`
	EnabledState         uint16 `cim:"EnabledState"`
	HealthState          uint16 `cim:"HealthState"` // 5=OK
	InstallDate          string `cim:"InstallDate"` // CIM DateTime（文字列）
	OnTimeInMilliseconds uint64 `cim:"OnTimeInMilliseconds"`
}

// VirtualDiskFormat 定数（CIM 仕様: Msvm_VirtualHardDiskSettingData.Format）
const (
	VHDFormatUnknown uint16 = 0
	VHDFormatVHD     uint16 = 2
	VHDFormatVHDX    uint16 = 3
	VHDFormatVHDSet  uint16 = 4
)

// VirtualDiskType 定数（CIM 仕様: Msvm_VirtualHardDiskSettingData.Type）
const (
	VHDTypeUnknown      uint16 = 0
	VHDTypeFixed        uint16 = 2 // 固定サイズ
	VHDTypeDynamic      uint16 = 3 // 動的拡張
	VHDTypeDifferencing uint16 = 4 // 差分
)

// Msvm_VirtualHardDiskSettingData は VHD/VHDX の設定情報を表す CIM クラス。
// Msvm_ImageManagementService の各メソッドで入出力に使用される。
type Msvm_VirtualHardDiskSettingData struct {
	InstanceID         string `cim:"InstanceID"`
	ElementName        string `cim:"ElementName"`
	VirtualDiskFormat  uint16 `cim:"VirtualDiskFormat"`  // 2=VHD, 3=VHDX, 4=VHDSet
	VirtualDiskType    uint16 `cim:"VirtualDiskType"`    // 2=Fixed, 3=Dynamic, 4=Differencing
	BlockSize          uint32 `cim:"BlockSize"`          // バイト単位
	LogicalSectorSize  uint32 `cim:"LogicalSectorSize"`  // バイト単位
	PhysicalSectorSize uint32 `cim:"PhysicalSectorSize"` // バイト単位
	MaxInternalSize    uint64 `cim:"MaxInternalSize"`    // バイト単位、論理サイズ
	Path               string `cim:"Path"`               // VHD ファイルパス
	ParentPath         string `cim:"ParentPath"`         // 差分ディスクの親ファイル
}
