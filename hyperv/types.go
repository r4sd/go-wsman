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

// VirtualSystemType 定数（Msvm_VirtualSystemSettingData.VirtualSystemType）
//
// 同一 VM に対して複数の SettingData が存在する。VM の現在の構成を取得するには
// Realized を使う（Snapshot/Planned はチェックポイント・予定設定の表現）。
const (
	VirtualSystemTypeRealized         = "Microsoft:Hyper-V:System:Realized"
	VirtualSystemTypeSnapshotRealized = "Microsoft:Hyper-V:System:Snapshot:Realized"
	VirtualSystemTypePlanned          = "Microsoft:Hyper-V:System:Planned"
)

// VirtualSystemSubType 定数（Msvm_VirtualSystemSettingData.VirtualSystemSubType）
//
// Hyper-V の世代を表す。Generation 2 は UEFI ブート対応。
const (
	VirtualSystemSubTypeGen1 = "Microsoft:Hyper-V:SubType:1"
	VirtualSystemSubTypeGen2 = "Microsoft:Hyper-V:SubType:2"
)

// AutomaticStartupAction 定数（CIM 仕様）
const (
	AutomaticStartupActionNone                       uint16 = 2
	AutomaticStartupActionRestartIfPreviouslyRunning uint16 = 3
	AutomaticStartupActionAlways                     uint16 = 4
)

// AutomaticShutdownAction 定数（CIM 仕様）
const (
	AutomaticShutdownActionTurnOff   uint16 = 2
	AutomaticShutdownActionSaveState uint16 = 3
	AutomaticShutdownActionShutDown  uint16 = 4
)

// AutomaticRecoveryAction 定数（CIM 仕様）
const (
	AutomaticRecoveryActionNone             uint16 = 2
	AutomaticRecoveryActionRestart          uint16 = 3
	AutomaticRecoveryActionRevertToSnapshot uint16 = 4
)

// Msvm_VirtualSystemSettingData は VM の構成設定を表す CIM クラス。
// 1 つの VM（Msvm_ComputerSystem）に対して、Realized / Snapshot:Realized 等
// 複数の SettingData が紐づく。VM の現在構成は VirtualSystemType="Realized" のもの。
//
// 配列プロパティ（BootSourceOrder, Notes 等）は Phase 1 では未対応のため除外。
type Msvm_VirtualSystemSettingData struct {
	InstanceID                  string `cim:"InstanceID"`
	ElementName                 string `cim:"ElementName"` // VM 表示名
	Caption                     string `cim:"Caption"`
	Description                 string `cim:"Description"`
	VirtualSystemIdentifier     string `cim:"VirtualSystemIdentifier"` // VM GUID（Msvm_ComputerSystem.Name と一致）
	VirtualSystemType           string `cim:"VirtualSystemType"`       // "Microsoft:Hyper-V:System:Realized" 等
	VirtualSystemSubType        string `cim:"VirtualSystemSubType"`    // "Microsoft:Hyper-V:SubType:1" or :2
	ConfigurationID             string `cim:"ConfigurationID"`         // 永続的な構成 ID
	ConfigurationDataRoot       string `cim:"ConfigurationDataRoot"`
	ConfigurationFile           string `cim:"ConfigurationFile"`
	SnapshotDataRoot            string `cim:"SnapshotDataRoot"`
	SuspendDataRoot             string `cim:"SuspendDataRoot"`
	SwapFileDataRoot            string `cim:"SwapFileDataRoot"`
	LogDataRoot                 string `cim:"LogDataRoot"`
	AutomaticStartupAction      uint16 `cim:"AutomaticStartupAction"`
	AutomaticStartupActionDelay string `cim:"AutomaticStartupActionDelay"` // CIM Duration（文字列）
	AutomaticShutdownAction     uint16 `cim:"AutomaticShutdownAction"`
	AutomaticRecoveryAction     uint16 `cim:"AutomaticRecoveryAction"`
	BIOSGUID                    string `cim:"BIOSGUID"`
	BIOSNumLock                 bool   `cim:"BIOSNumLock"`
	SecureBoot                  bool   `cim:"SecureBoot"`
	SecureBootTemplateId        string `cim:"SecureBootTemplateId"`
	Version                     string `cim:"Version"`      // 構成バージョン（例: "10.0"）
	CreationTime                string `cim:"CreationTime"` // CIM DateTime（文字列）
}
