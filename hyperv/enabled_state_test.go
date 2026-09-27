package hyperv

import "testing"

// TestEnabledStateConstants は Msvm_ComputerSystem.EnabledState の定数を実機値に固定する (#102)。
//
// 出どころ: 2026-09-27 に使い捨て Gen2 VM を作り、RequestStateChange で状態を動かして
// EnabledState を読んだ実測。2026-07-09 の採取 (PS Get-CimInstance とのクロスチェック付き)
// とも一致する。観測できたのは 2/3/6/9 の 4 値のみ。
//
// ⚠️ このテストの性質: これは**変更検知器**であって実機との整合を担保しない。
// 値を書き換えれば落ちるだけで、実機が本当に 9/6 を返すかは確認していない。
// 置いてある意味は「出どころを doc として残す」ことと「書き換えに摩擦を作る」こと。
// 実機整合を担保するには Paused/Saved 状態の応答を記録した golden が要る。
//
// 32768/32769 は Hyper-V v1 の値。MS の v2 ページにも v1 由来の記述が残っているため
// (HealthState 節・OperationalStatus 節)、それを拾って定数にしたのが元の誤り。
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
// EnabledState と**同じ数値**を使う。Hyper-V v1 の 32768/32769 を渡すと実機は
// ReturnValue=32775 (invalid state for this operation) を返して遷移しない
// (2026-09-27 に実機 1 台で確認。Running 安定後に 6 回リトライしても全て 32775)。
//
// wire に載る値は TestClient_StateShortcuts (vm_state_test.go) でも固定している。
// 値を変えるときは両方直すこと。
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
