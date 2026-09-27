package hyperv

import "testing"

// TestEnabledStateConstants は Msvm_ComputerSystem.EnabledState の定数を実機値に固定する (#102)。
//
// 出どころ: 2026-09-27 に使い捨て Gen2 VM を作り、RequestStateChange で状態を動かして
// EnabledState を読んだ実測。2026-07-09 の採取 (PS Get-CimInstance とのクロスチェック付き)
// とも一致する。
//
// 🔴 CIM 標準ドキュメントの 32768/32769 を入れると落ちる。**Msvm_ComputerSystem では
// それらは返らない。** 標準値をそのまま定数にした思い込みが元の誤りだった。
func TestEnabledStateConstants(t *testing.T) {
	tests := []struct {
		name  string
		got   uint16
		want  uint16
		state string
	}{
		{"Enabled", EnabledStateEnabled, 2, "Running"},
		{"Disabled", EnabledStateDisabled, 3, "Off"},
		{"Paused", EnabledStatePaused, 9, "Paused (Suspend-VM)"},
		{"Saved", EnabledStateSaved, 6, "Saved (Save-VM)"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("EnabledState%s = %d, want %d (実機の %s)", tt.name, tt.got, tt.want, tt.state)
		}
	}
}

// TestRequestedStateConstants は RequestStateChange に渡す値を実機で通った値に固定する。
//
// EnabledState と**同じ数値**を使う。v1 名前空間の 32768/32769 を渡すと実機は
// ReturnValue=32775 (invalid state for this operation) を返して遷移しない
// (2026-09-27 実測。6 回リトライしても全て 32775)。
func TestRequestedStateConstants(t *testing.T) {
	tests := []struct {
		name string
		got  uint16
		want uint16
	}{
		{"Enabled", RequestedStateEnabled, 2},
		{"Disabled", RequestedStateDisabled, 3},
		{"Paused", RequestedStatePaused, 9},
		{"Saved", RequestedStateSaved, 6},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("RequestedState%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}
