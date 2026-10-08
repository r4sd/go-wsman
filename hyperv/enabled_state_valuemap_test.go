package hyperv

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// enabled_state_valuemap_test.go は EnabledState* 定数が
// Msvm_ComputerSystem.EnabledState の ValueMap 上で**予約枠でない名前付きの値**に
// 着地することを検証する (#171 / #102 の再発防止)。
//
// # この検査で担保できること / できないこと
//
// 担保できる: 定数の値が CIM クラスが名前を与えている値であること。
// 担保できない: その値の **Hyper-V での意味**。ValueMap はジェネリック CIM の名前しか
// 持たないので、「Quiesce = Paused」「Enabled but Offline = Saved」までは固定できない。
// そこは実機観測が根拠で、hyperv/types.go のコメントに書いてある。
//
// # なぜ「ValueMap の範囲内か」ではなく「予約枠でない名前か」なのか
//
// 🔴 **範囲検査では #102 を捕まえられない。** 実機の ValueMap は
// `32768..65535` (Vendor Reserved) を含んでいるので、#102 で入ってしまった
// 32768 / 32769 も「範囲内」として通る。
//
// Issue #171 は「突合できるのは値が ValueMap の範囲内かまで」としていたが、
// それだと**この検査が存在しても #102 は起きていた**。予約枠を除外して初めて
// 非自明な検査になる。TestEnabledStateValueMapRejectsReservedValues が
// その非空虚性を固定している。
type cimValueMapEntry struct {
	Low, High uint16 // 単一値なら Low == High
	Name      string
}

// loadValueMapFixture は testdata/mof/{filename} から ValueMap を読み込む。
// 1 行 1 エントリ、`<値 または Low..High> <名前>`。空行と '#' 始まりは無視。
func loadValueMapFixture(t *testing.T, filename string) []cimValueMapEntry {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "mof", filename))
	if err != nil {
		t.Fatalf("ValueMap fixture を開けません: %v", err)
	}
	defer f.Close()

	var out []cimValueMapEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, name, ok := strings.Cut(line, " ")
		if !ok || strings.TrimSpace(name) == "" {
			t.Fatalf("ValueMap fixture %s: 不正な行 %q (期待: <値|Low..High> <名前>)", filename, line)
		}
		parse := func(s string) uint16 {
			n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 16)
			if err != nil {
				t.Fatalf("ValueMap fixture %s: 値を読めません %q: %v", filename, line, err)
			}
			return uint16(n)
		}
		e := cimValueMapEntry{Name: strings.TrimSpace(name)}
		if lo, hi, isRange := strings.Cut(key, ".."); isRange {
			e.Low, e.High = parse(lo), parse(hi)
			if e.Low > e.High {
				t.Fatalf("ValueMap fixture %s: 範囲が逆 %q", filename, line)
			}
		} else {
			e.Low = parse(key)
			e.High = e.Low
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("ValueMap fixture スキャン失敗: %v", err)
	}
	if len(out) == 0 {
		t.Fatalf("ValueMap fixture %s が空", filename)
	}
	return out
}

// lookup は値に対応する名前を返す。どのエントリにも当たらなければ ok=false。
func lookupValueMap(entries []cimValueMapEntry, v uint16) (string, bool) {
	for _, e := range entries {
		if e.Low <= v && v <= e.High {
			return e.Name, true
		}
	}
	return "", false
}

// isReservedCIMValueName は CIM の予約枠の名前かを返す。
// 予約枠は「クラスが意味を与えていない」ことを意味するので、
// 定数がここに着地したら実装の誤りとして扱う。
func isReservedCIMValueName(name string) bool {
	return strings.Contains(name, "Reserved")
}

// TestEnabledStateConstantsAreNamedValues は EnabledState* 定数が
// ValueMap 上で予約枠でない名前に着地することを検証する。
func TestEnabledStateConstantsAreNamedValues(t *testing.T) {
	entries := loadValueMapFixture(t, "msvm_computersystem_enabledstate_valuemap.txt")

	// wantCIMName はジェネリック CIM の名前。Hyper-V の意味は hypervMeaning に書く
	// (ValueMap では証明できないので、assert の対象にはしない)。
	cases := []struct {
		constName     string
		value         uint16
		wantCIMName   string
		hypervMeaning string
	}{
		{"EnabledStateUnknown", EnabledStateUnknown, "Unknown", "不明"},
		{"EnabledStateEnabled", EnabledStateEnabled, "Enabled", "Running"},
		{"EnabledStateDisabled", EnabledStateDisabled, "Disabled", "Off"},
		{"EnabledStatePaused", EnabledStatePaused, "Quiesce", "Paused (Suspend-VM)"},
		{"EnabledStateSaved", EnabledStateSaved, "Enabled but Offline", "Saved (Save-VM)"},
	}
	for _, c := range cases {
		t.Run(c.constName, func(t *testing.T) {
			name, ok := lookupValueMap(entries, c.value)
			if !ok {
				t.Fatalf("%s = %d は ValueMap のどのエントリにも当たらない", c.constName, c.value)
			}
			if isReservedCIMValueName(name) {
				t.Fatalf("%s = %d は予約枠 %q に落ちている。"+
					"クラスが意味を与えていない値を定数にしている (#102 と同型)",
					c.constName, c.value, name)
			}
			if name != c.wantCIMName {
				t.Errorf("%s = %d のジェネリック CIM 名が %q、期待 %q (Hyper-V での意味: %s)",
					c.constName, c.value, name, c.wantCIMName, c.hypervMeaning)
			}
		})
	}
}

// TestEnabledStateValueMapRejectsReservedValues は上の検査が**非空虚**であることを固定する。
//
// #102 で実際に入ってしまった 32768 / 32769 が予約枠に落ちること、つまり
// この検査があれば #102 は落ちていたことを確かめる。
// あわせて「範囲内か」だけでは通ってしまうことも示す (ここが Issue #171 の記述との差)。
func TestEnabledStateValueMapRejectsReservedValues(t *testing.T) {
	entries := loadValueMapFixture(t, "msvm_computersystem_enabledstate_valuemap.txt")

	for _, v := range []uint16{32768, 32769} {
		name, ok := lookupValueMap(entries, v)
		if !ok {
			t.Fatalf("%d が ValueMap に当たらない。"+
				"予約枠 (32768..65535) を fixture から落とすと "+
				"「範囲外」と「予約枠」が区別できなくなる", v)
		}
		if !isReservedCIMValueName(name) {
			t.Errorf("%d が予約枠と判定されない (名前 %q)。"+
				"この検査は #102 を捕まえられない", v, name)
		}
	}

	// 名前付きの値が予約枠と誤判定されないこと (逆方向)。
	for _, v := range []uint16{EnabledStatePaused, EnabledStateSaved} {
		name, ok := lookupValueMap(entries, v)
		if !ok || isReservedCIMValueName(name) {
			t.Errorf("%d が予約枠と誤判定された (名前 %q, ok=%v)", v, name, ok)
		}
	}
}

// TestEnabledStateValueMapFixtureCoversWholeRange は fixture が uint16 全域を
// 隙間なく覆っていることを検証する。
//
// 覆っていないと「ValueMap に当たらない」が「fixture の書き漏れ」なのか
// 「本当に定義外」なのか区別できず、上の検査が静かに空振りする。
func TestEnabledStateValueMapFixtureCoversWholeRange(t *testing.T) {
	entries := loadValueMapFixture(t, "msvm_computersystem_enabledstate_valuemap.txt")
	for _, v := range []uint16{0, 10, 11, 32767, 32768, 65535} {
		if _, ok := lookupValueMap(entries, v); !ok {
			t.Errorf("%d が fixture のどのエントリにも当たらない (全域を覆っていない)", v)
		}
	}
}
