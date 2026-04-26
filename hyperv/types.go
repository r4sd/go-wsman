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
