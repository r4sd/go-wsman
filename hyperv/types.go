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
	VirtualDiskFormat  uint16 `cim:"Format"`             // 2=VHD, 3=VHDX, 4=VHDSet (CIM 正名: Format)
	VirtualDiskType    uint16 `cim:"Type"`               // 2=Fixed, 3=Dynamic, 4=Differencing (CIM 正名: Type)
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

// AutomaticCriticalErrorAction 定数（Msvm_VirtualSystemSettingData.AutomaticCriticalErrorAction）
//
// クリティカルエラー（例: ストレージ切断）発生時の VM の動作。
const (
	AutomaticCriticalErrorActionNone  uint16 = 0
	AutomaticCriticalErrorActionPause uint16 = 1
)

// NetworkBootPreferredProtocol 定数（Msvm_VirtualSystemSettingData.NetworkBootPreferredProtocol）。
//
// Gen2 VM の PXE ブート時に使うプロトコル。MOF の ValueMap は 4096/4097 で
// 直感的な 4/6 ではない点に注意 (Issue #51 の表は誤り、MOF を一次資料とした)。
const (
	NetworkBootPreferredProtocolIPv4 uint16 = 4096
	NetworkBootPreferredProtocolIPv6 uint16 = 4097
)

// ConsoleMode 定数（Msvm_VirtualSystemSettingData.ConsoleMode）。
//
// VM のコンソール出力先。COM1/COM2 を指定するとシリアル経由で OS の
// コンソールにアクセスできる (Linux ゲストの早期ブートデバッグ等で利用)。
const (
	ConsoleModeDefault uint16 = 0
	ConsoleModeCOM1    uint16 = 1
	ConsoleModeCOM2    uint16 = 2
	ConsoleModeNone    uint16 = 3
)

// ResourceType 定数（CIM_ResourceAllocationSettingData.ResourceType）
//
// VM に紐づくリソース種別を表す。Phase 4 で扱うリソースに対応する値のみ列挙する。
const (
	ResourceTypeIDEController      uint16 = 5  // IDE Controller
	ResourceTypeProcessor          uint16 = 3  // CPU
	ResourceTypeMemory             uint16 = 4  // メモリ
	ResourceTypeParallelSCSI       uint16 = 6  // SCSI Controller (Parallel SCSI HBA)
	ResourceTypeEthernetAdapter    uint16 = 10 // NIC (Synthetic Ethernet Port)
	ResourceTypeDVDDrive           uint16 = 16 // DVD ドライブ
	ResourceTypeDiskDrive          uint16 = 17 // ディスクドライブ
	ResourceTypeStorageExtent      uint16 = 31 // ストレージ (VHD/ISO ファイル)
	ResourceTypeEthernetConnection uint16 = 33 // NIC とスイッチの接続 (Ethernet Port Allocation)
)

// ResourceSubType 定数（Msvm_*SettingData.ResourceSubType）
//
// CIM の ResourceType だけでは Hyper-V のリソース種別が一意に決まらないため、
// Hyper-V 独自のサブタイプ文字列で区別する。Embedded Instance 作成時に必須。
const (
	ResourceSubTypeSyntheticEthernetPort = "Microsoft:Hyper-V:Synthetic Ethernet Port"
	ResourceSubTypeEthernetConnection    = "Microsoft:Hyper-V:Ethernet Connection"

	// ストレージ系
	ResourceSubTypeIDEController      = "Microsoft:Hyper-V:Emulated IDE Controller"
	ResourceSubTypeSCSIController     = "Microsoft:Hyper-V:Synthetic SCSI Controller"
	ResourceSubTypeSyntheticDiskDrive = "Microsoft:Hyper-V:Synthetic Disk Drive"
	ResourceSubTypeSyntheticDVDDrive  = "Microsoft:Hyper-V:Synthetic DVD Drive"
	ResourceSubTypeVirtualHardDisk    = "Microsoft:Hyper-V:Virtual Hard Disk"
	ResourceSubTypeVirtualCDDVDDisk   = "Microsoft:Hyper-V:Virtual CD/DVD Disk"
)

// Msvm_MemorySettingData は VM のメモリ設定を表す CIM クラス。
//
// VM 作成時に Hyper-V がデフォルト値で初期化する。変更は ModifyResourceSettings
// 経由で行う (本パッケージの SetMemorySettings ヘルパーを利用)。
type Msvm_MemorySettingData struct {
	InstanceID           string `cim:"InstanceID"`
	ElementName          string `cim:"ElementName"`
	ResourceType         uint16 `cim:"ResourceType"`         // 4 = Memory
	VirtualQuantity      uint64 `cim:"VirtualQuantity"`      // 割り当てメモリ (MB)
	DynamicMemoryEnabled bool   `cim:"DynamicMemoryEnabled"` // 動的メモリ
	Reservation          uint64 `cim:"Reservation"`          // 最小メモリ (MB)
	Limit                uint64 `cim:"Limit"`                // 最大メモリ (MB)
	Weight               uint32 `cim:"Weight"`               // メモリ重み (1〜10000、デフォルト 5000)
}

// Msvm_ProcessorSettingData は VM の CPU 設定を表す CIM クラス。
//
// VM 作成時に Hyper-V がデフォルト値 (1 vCPU 等) で初期化する。
type Msvm_ProcessorSettingData struct {
	InstanceID                     string `cim:"InstanceID"`
	ElementName                    string `cim:"ElementName"`
	ResourceType                   uint16 `cim:"ResourceType"`                   // 3 = Processor
	VirtualQuantity                uint64 `cim:"VirtualQuantity"`                // vCPU 数
	Reservation                    uint64 `cim:"Reservation"`                    // CPU 予約 (0〜100000、% × 1000)
	Limit                          uint64 `cim:"Limit"`                          // CPU 上限 (0〜100000、デフォルト 100000)
	Weight                         uint32 `cim:"Weight"`                         // CPU 重み (デフォルト 100)
	LimitProcessorFeatures         bool   `cim:"LimitProcessorFeatures"`         // 機能制限 (移行時の互換性)
	LimitCPUID                     bool   `cim:"LimitCPUID"`                     // CPUID 制限
	ExposeVirtualizationExtensions bool   `cim:"ExposeVirtualizationExtensions"` // ネステッド仮想化
	HwThreadsPerCore               uint64 `cim:"HwThreadsPerCore"`               // コアあたり SMT スレッド数 (0=ホスト既定)
	MaxProcessorsPerNumaNode       uint64 `cim:"MaxProcessorsPerNumaNode"`       // 仮想 NUMA ノードあたり最大 vCPU 数
	MaxNumaNodesPerSocket          uint64 `cim:"MaxNumaNodesPerSocket"`          // ソケットあたり最大 NUMA ノード数
	EnableHostResourceProtection   bool   `cim:"EnableHostResourceProtection"`   // ホストリソース保護
}

// Msvm_GpuPartitionSettingData は VM に割り当てられた GPU パーティション (GPU-PV) の設定を表す CIM クラス。
//
// CIM_ResourceAllocationSettingData を継承する (RASD)。GPU パーティション分割の割当範囲を
// VRAM/Encode/Decode/Compute の Min/Max/Optimal で表す。全プロパティ uint64 (MOF 準拠)。
// InstanceID は基底クラス由来で、VM 単位の絞り込み (matchSettingDataVM) に使う。
//
// enumerate はホスト能力定義 (InstanceID="Microsoft:Definition\<GPU_GUID>\Default|Minimum|Maximum|
// Increment") も返すが、これらは VM_GUID 前方一致で自然に除外される (2026-07-16 実機プローブで確認)。
type Msvm_GpuPartitionSettingData struct {
	InstanceID              string `cim:"InstanceID"`
	MinPartitionVRAM        uint64 `cim:"MinPartitionVRAM"`
	MaxPartitionVRAM        uint64 `cim:"MaxPartitionVRAM"`
	OptimalPartitionVRAM    uint64 `cim:"OptimalPartitionVRAM"`
	MinPartitionEncode      uint64 `cim:"MinPartitionEncode"`
	MaxPartitionEncode      uint64 `cim:"MaxPartitionEncode"`
	OptimalPartitionEncode  uint64 `cim:"OptimalPartitionEncode"`
	MinPartitionDecode      uint64 `cim:"MinPartitionDecode"`
	MaxPartitionDecode      uint64 `cim:"MaxPartitionDecode"`
	OptimalPartitionDecode  uint64 `cim:"OptimalPartitionDecode"`
	MinPartitionCompute     uint64 `cim:"MinPartitionCompute"`
	MaxPartitionCompute     uint64 `cim:"MaxPartitionCompute"`
	OptimalPartitionCompute uint64 `cim:"OptimalPartitionCompute"`
}

// Msvm_VirtualEthernetSwitch は Hyper-V 仮想スイッチを表す CIM クラス。
type Msvm_VirtualEthernetSwitch struct {
	Name        string `cim:"Name"`        // スイッチ GUID
	ElementName string `cim:"ElementName"` // 表示名 (terraform でいう switch_name)
	Description string `cim:"Description"`
	HealthState uint16 `cim:"HealthState"` // 5=OK
}

// Msvm_VirtualEthernetSwitchSettingData は仮想スイッチの構成設定。
//
// CreateSwitch 時に Embedded Instance として送信する。
type Msvm_VirtualEthernetSwitchSettingData struct {
	InstanceID   string `cim:"InstanceID"`
	ElementName  string `cim:"ElementName"`  // スイッチ表示名
	Notes        string `cim:"Notes"`        // 配列だが Phase 5 では単一値で扱う
	IOVPreferred bool   `cim:"IOVPreferred"` // SR-IOV 優先
}

// Msvm_ExternalEthernetPort は Hyper-V ホストの物理 NIC を表す。
//
// External Switch を作成するときに、接続先となる物理 NIC を識別するために使う。
// read-only。
type Msvm_ExternalEthernetPort struct {
	Name             string `cim:"Name"`             // GUID
	ElementName      string `cim:"ElementName"`      // 表示名 (例: "Realtek Gaming 2.5GbE")
	DeviceID         string `cim:"DeviceID"`         // 物理デバイス ID
	PermanentAddress string `cim:"PermanentAddress"` // 永続 MAC アドレス
	IsBound          bool   `cim:"IsBound"`          // 既にスイッチに紐付け済みか
}

// Msvm_SyntheticEthernetPortSettingData は VM の合成 NIC 設定を表す CIM クラス。
//
// 単独で AddResourceSettings すると VM に NIC 本体だけ追加される (スイッチに
// 接続されていない無効状態)。スイッチへの接続には Msvm_EthernetPortAllocationSettingData
// を別途 Add する必要がある。
type Msvm_SyntheticEthernetPortSettingData struct {
	InstanceID        string `cim:"InstanceID"`
	ElementName       string `cim:"ElementName"`      // NIC 表示名 (任意)
	ResourceType      uint16 `cim:"ResourceType"`     // 10
	ResourceSubType   string `cim:"ResourceSubType"`  // ResourceSubTypeSyntheticEthernetPort
	StaticMacAddress  bool   `cim:"StaticMacAddress"` // false なら動的 MAC
	Address           string `cim:"Address"`          // MAC アドレス (12 桁 hex、区切り文字なし)
	AllowPacketDirect bool   `cim:"AllowPacketDirect"`
	ClusterMonitored  bool   `cim:"ClusterMonitored"`
}

// Msvm_EthernetPortAllocationSettingData は NIC と仮想スイッチの接続を表す CIM クラス。
//
// Parent: 親 NIC (Msvm_SyntheticEthernetPortSettingData) の EPR
// HostResource: 接続先スイッチ (Msvm_VirtualEthernetSwitch) の EPR
//
// HostResource は CIM 仕様では string[] だが、Phase 4 では 1 要素のみのケースで
// 単一文字列として扱う (実機の Hyper-V も 1 要素送信を受理する)。
type Msvm_EthernetPortAllocationSettingData struct {
	InstanceID      string `cim:"InstanceID"`
	ElementName     string `cim:"ElementName"`
	ResourceType    uint16 `cim:"ResourceType"`    // 33
	ResourceSubType string `cim:"ResourceSubType"` // ResourceSubTypeEthernetConnection
	HostResource    string `cim:"HostResource"`    // 接続先スイッチ EPR
	Parent          string `cim:"Parent"`          // 親 NIC の EPR
	EnabledState    uint16 `cim:"EnabledState"`    // 2=Enabled, 3=Disabled
}

// Msvm_ResourceAllocationSettingData は VM に割り当てられた汎用リソースを表す。
//
// Hyper-V では Controller (IDE/SCSI) と Drive (Disk/DVD) を同じクラスで表現する。
// ResourceType + ResourceSubType の組み合わせで具体的な種別を識別する。
//
// Drive (Disk/DVD) を Controller に接続する場合:
//   - Parent: 親 Controller の EPR
//   - AddressOnParent: Controller 内の位置 ("0", "1" など)
type Msvm_ResourceAllocationSettingData struct {
	InstanceID      string `cim:"InstanceID"`
	ElementName     string `cim:"ElementName"`
	ResourceType    uint16 `cim:"ResourceType"`
	ResourceSubType string `cim:"ResourceSubType"`
	Address         string `cim:"Address"`
	AddressOnParent string `cim:"AddressOnParent"` // Controller 内の location (Drive の場合)
	Parent          string `cim:"Parent"`          // 親 Controller/Drive の EPR
	HostResource    string `cim:"HostResource"`
	VirtualQuantity uint64 `cim:"VirtualQuantity"`
}

// Msvm_StorageAllocationSettingData は VHD/ISO ファイルの Drive へのマッピングを表す。
//
// Hyper-V では VHD と ISO の両方をこのクラスで扱い、ResourceSubType で区別する:
//   - "Microsoft:Hyper-V:Virtual Hard Disk" → VHD
//   - "Microsoft:Hyper-V:Virtual CD/DVD Disk" → ISO
//
// Parent: Drive (Msvm_ResourceAllocationSettingData) の EPR
// HostResource: ファイルパス (string[1] だが 1 要素のみ扱う)
type Msvm_StorageAllocationSettingData struct {
	InstanceID      string `cim:"InstanceID"`
	ElementName     string `cim:"ElementName"`
	ResourceType    uint16 `cim:"ResourceType"`    // 31 (StorageExtent)
	ResourceSubType string `cim:"ResourceSubType"` // VirtualHardDisk or VirtualCDDVDDisk
	HostResource    string `cim:"HostResource"`    // ファイルパス
	Parent          string `cim:"Parent"`          // Drive の EPR
	Address         string `cim:"Address"`
}

// Msvm_VirtualSystemSettingData は VM の構成設定を表す CIM クラス。
// 1 つの VM（Msvm_ComputerSystem）に対して、Realized / Snapshot:Realized 等
// 複数の SettingData が紐づく。VM の現在構成は VirtualSystemType="Realized" のもの。
//
// 配列プロパティ (BootSourceOrder / Notes) は #48 配列対応基盤 + UnmarshalList で対応済。
type Msvm_VirtualSystemSettingData struct {
	InstanceID                          string   `cim:"InstanceID"`
	ElementName                         string   `cim:"ElementName"` // VM 表示名
	Caption                             string   `cim:"Caption"`
	Description                         string   `cim:"Description"`
	VirtualSystemIdentifier             string   `cim:"VirtualSystemIdentifier"` // VM GUID（Msvm_ComputerSystem.Name と一致）
	VirtualSystemType                   string   `cim:"VirtualSystemType"`       // "Microsoft:Hyper-V:System:Realized" 等
	VirtualSystemSubType                string   `cim:"VirtualSystemSubType"`    // "Microsoft:Hyper-V:SubType:1" or :2
	ConfigurationID                     string   `cim:"ConfigurationID"`         // 永続的な構成 ID
	ConfigurationDataRoot               string   `cim:"ConfigurationDataRoot"`
	ConfigurationFile                   string   `cim:"ConfigurationFile"`
	SnapshotDataRoot                    string   `cim:"SnapshotDataRoot"`
	SuspendDataRoot                     string   `cim:"SuspendDataRoot"`
	SwapFileDataRoot                    string   `cim:"SwapFileDataRoot"`
	LogDataRoot                         string   `cim:"LogDataRoot"`
	AutomaticStartupAction              uint16   `cim:"AutomaticStartupAction"`
	AutomaticStartupActionDelay         string   `cim:"AutomaticStartupActionDelay"` // CIM Duration（文字列）
	AutomaticShutdownAction             uint16   `cim:"AutomaticShutdownAction"`
	AutomaticRecoveryAction             uint16   `cim:"AutomaticRecoveryAction"`
	AutomaticCriticalErrorAction        uint16   `cim:"AutomaticCriticalErrorAction"`        // 0=None, 1=Pause
	AutomaticCriticalErrorActionTimeout string   `cim:"AutomaticCriticalErrorActionTimeout"` // CIM datetime (interval)、Pause 継続時間
	BIOSGUID                            string   `cim:"BIOSGUID"`
	BIOSNumLock                         bool     `cim:"BIOSNumLock"`
	SecureBoot                          bool     `cim:"SecureBootEnabled"` // CIM 正名: SecureBootEnabled
	SecureBootTemplateId                string   `cim:"SecureBootTemplateId"`
	BootSourceOrder                     []string `cim:"BootSourceOrder"` // Gen2 ブート順序 (EPR/Drive 参照の配列)
	Notes                               []string `cim:"Notes"`           // VM 備考 (複数行を配列で保持)
	LockOnDisconnect                    bool     `cim:"LockOnDisconnect"`
	GuestControlledCacheTypes           bool     `cim:"GuestControlledCacheTypes"`
	HighMmioGapSize                     uint64   `cim:"HighMmioGapSize"`           // High MMIO ギャップサイズ (MB)
	LowMmioGapSize                      uint64   `cim:"LowMmioGapSize"`            // Low MMIO ギャップサイズ (MB)
	AutomaticSnapshotsEnabled           bool     `cim:"AutomaticSnapshotsEnabled"` // 自動スナップショット (Win10+ ホスト)
	// Gen2 ファームウェア (#51)
	NetworkBootPreferredProtocol uint16 `cim:"NetworkBootPreferredProtocol"` // 4096=IPv4, 4097=IPv6 (CIM 列挙値、4/6 ではない)
	ConsoleMode                  uint16 `cim:"ConsoleMode"`                  // 0=Default, 1=COM1, 2=COM2, 3=None
	PauseAfterBootFailure        bool   `cim:"PauseAfterBootFailure"`        // BIOS が boot 失敗で一時停止 (MOF は boolean、OnOffState ではない)
	Version                      string `cim:"Version"`                      // 構成バージョン（例: "10.0"）
	CreationTime                 string `cim:"CreationTime"`                 // CIM DateTime（文字列）
}

// VlanOperationMode 定数群 (Msvm_EthernetSwitchPortVlanSettingData.OperationMode、CIM 型 uint32)。
// MOF デフォルトは 0 (タグなし扱い)。明示する場合は下記を使う。
const (
	VlanOperationModeAccess  uint32 = 1
	VlanOperationModeTrunk   uint32 = 2
	VlanOperationModePrivate uint32 = 3
)

// PvlanMode 定数群 (Msvm_EthernetSwitchPortVlanSettingData.PvlanMode、CIM 型 uint32)。
// Private VLAN 利用時のみ意味を持つ。
const (
	PvlanModeIsolated    uint32 = 1
	PvlanModeCommunity   uint32 = 2
	PvlanModePromiscuous uint32 = 3
)

// Msvm_EthernetSwitchPortVlanSettingData は仮想 NIC ポートの VLAN 設定を表す CIM クラス。
// AddFeatureSettings 経由で NIC (Msvm_EthernetPortAllocationSettingData) に紐付ける。
//
// OperationMode によって意味を持つフィールドが切り替わる:
//   - Access (1)  → AccessVlanId
//   - Trunk (2)   → NativeVlanId + TrunkVlanIdArray + (任意) PruneVlanIdArray
//   - Private (3) → PvlanMode + PrimaryVlanId + SecondaryVlanId(Array)
//
// CIM 仕様: https://learn.microsoft.com/en-us/windows/win32/hyperv_v2/msvm-ethernetswitchportvlansettingdata
type Msvm_EthernetSwitchPortVlanSettingData struct {
	InstanceID           string   `cim:"InstanceID"`
	OperationMode        uint32   `cim:"OperationMode"` // 1=Access, 2=Trunk, 3=Private
	AccessVlanId         uint16   `cim:"AccessVlanId"`
	NativeVlanId         uint16   `cim:"NativeVlanId"`
	PvlanMode            uint32   `cim:"PvlanMode"` // 1=Isolated, 2=Community, 3=Promiscuous
	PrimaryVlanId        uint16   `cim:"PrimaryVlanId"`
	SecondaryVlanId      uint16   `cim:"SecondaryVlanId"`
	PruneVlanIdArray     []uint16 `cim:"PruneVlanIdArray"`
	TrunkVlanIdArray     []uint16 `cim:"TrunkVlanIdArray"`
	SecondaryVlanIdArray []uint16 `cim:"SecondaryVlanIdArray"`
}

// JobState 定数（CIM_ConcreteJob.JobState、Msvm_ConcreteJob が継承）。
//
// 終端状態: Completed=成功、Terminated/Killed/Exception=失敗。
// それ以外 (New〜Service) は進行中。
const (
	JobStateNew          uint16 = 2
	JobStateStarting     uint16 = 3
	JobStateRunning      uint16 = 4
	JobStateSuspended    uint16 = 5
	JobStateShuttingDown uint16 = 6
	JobStateCompleted    uint16 = 7
	JobStateTerminated   uint16 = 8
	JobStateKilled       uint16 = 9
	JobStateException    uint16 = 10
	JobStateService      uint16 = 11
)

// Msvm_ConcreteJob は非同期操作の進捗を追跡する CIM クラス。
// 名前空間: root/virtualization/v2。
//
// 非同期 (ReturnValue=4096) を返す CIM メソッドの完了待ちに使う。InstanceID で
// Get し、JobState の終端到達を待つ (WaitForJob)。
type Msvm_ConcreteJob struct {
	InstanceID       string `cim:"InstanceID"`
	JobState         uint16 `cim:"JobState"`
	PercentComplete  uint16 `cim:"PercentComplete"`
	ErrorCode        uint16 `cim:"ErrorCode"`
	ErrorDescription string `cim:"ErrorDescription"`
}

// Msvm_GuestNetworkAdapterConfiguration はゲスト OS 内の NIC 設定 (IP アドレス等) を表す。
// 名前空間: root/virtualization/v2。
//
// wait_for_ips (PS の Wait-VM -For IPAddress 相当) の go-wsman 化に使う。IPAddresses は
// ゲスト OS から報告された実際の IP (DHCP/静的問わず)。InstanceID は実機確認済みで
// "Microsoft:GuestNetwork\<VM GUID>\<NIC 側 GUID>" 形式 (例:
// "Microsoft:GuestNetwork\27BBD2D0-EA18-44C5-96F6-2B2ACE17D7AD\F0D7A657-...")。
// 対応する Msvm_SyntheticEthernetPortSettingData.InstanceID は "Microsoft:<VM GUID>\<同じ NIC 側
// GUID>" で、"GuestNetwork\" が挟まる点だけが違う。この対応関係は Msvm_SettingDataComponent
// 経由で辿る (単純な文字列除去では不安定なため、GroupComponent/PartComponent の突き合わせを使う)。
//
// 実機確認 (2026-07-26、Talos VM 3台): ゲスト統合サービスの KVP 交換 (Hyper-V Data Exchange) を
// 実装しないゲスト OS (Talos Linux 等) では IPAddresses が常に空。ホストの Hyper-V 統合サービスが
// ゲスト側デーモンに依存するため、Windows ゲスト等 KVP 対応ゲストでのみ値が入る。
type Msvm_GuestNetworkAdapterConfiguration struct {
	InstanceID       string   `cim:"InstanceID"`
	ProtocolIFType   uint16   `cim:"ProtocolIFType"`
	DHCPEnabled      bool     `cim:"DHCPEnabled"`
	IPAddresses      []string `cim:"IPAddresses"`
	Subnets          []string `cim:"Subnets"`
	DefaultGateways  []string `cim:"DefaultGateways"`
	DNSServers       []string `cim:"DNSServers"`
	IPAddressOrigins []uint16 `cim:"IPAddressOrigins"`
}

// Msvm_SettingDataComponent は Msvm_SyntheticEthernetPortSettingData (または
// Msvm_EmulatedEthernetPortSettingData) と Msvm_GuestNetworkAdapterConfiguration を結ぶ
// association。名前空間: root/virtualization/v2。
//
// 実機確認 (2026-07-26): GroupComponent/PartComponent は CIM 上 REF プロパティだが、Parent/
// HostResource (Msvm_EthernetPortAllocationSettingData) と違い WMI オブジェクトパス文字列 (
// \\HOST\namespace:Class.InstanceID="...") ではなく、対象インスタンスの素の InstanceID がそのまま
// 返る (例: GroupComponent="Microsoft:27BBD2D0-...\F0D7A657-..."、PartComponent=
// "Microsoft:GuestNetwork\27BBD2D0-...\F0D7A657-..."。ともに対応インスタンスの InstanceID と完全
// 一致)。呼び出し側 (provider の抽出ヘルパー等) が Parent/HostResource と同じ WMI パス抽出処理を
// 通しても、該当パターンが無ければ入力をそのまま返す no-op になるため無害。
type Msvm_SettingDataComponent struct {
	GroupComponent string `cim:"GroupComponent"` // 親 NIC (Port SettingData) の InstanceID
	PartComponent  string `cim:"PartComponent"`  // Msvm_GuestNetworkAdapterConfiguration の InstanceID
}
