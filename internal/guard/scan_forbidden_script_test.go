package guard_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scanForbiddenScript は CI が使う走査スクリプト。
const scanForbiddenScript = "../../.github/scripts/scan_forbidden.sh"

// runScanText はスクリプトを text モードで動かし、終了コードと出力を返す。
func runScanText(t *testing.T, input string) (int, string) {
	t.Helper()
	abs, err := filepath.Abs(scanForbiddenScript)
	if err != nil {
		t.Fatalf("パスを解決できない: %v", err)
	}
	cmd := exec.Command("bash", abs, "text")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	code := 0
	var ee *exec.ExitError
	if err != nil {
		if ok := asExitError(err, &ee); !ok {
			t.Fatalf("スクリプトを実行できない: %v (出力: %s)", err, out)
		}
		code = ee.ExitCode()
	}
	return code, string(out)
}

func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// TestScanForbiddenScript_Text は CI の走査スクリプトの**負のテスト**。
//
// 🔴 これが無いと「検査がある」だけで「検査が効く」ことは誰も確かめていない (#159)。
// パターンを緩める変更が入っても気付けない。
//
// ⚠️ **検出すべき入力と、してはいけない入力の両方**を見る。片方だけだと
// 「常に検出する」実装や「常に通す」実装を区別できない。
func TestScanForbiddenScript_Text(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantFail bool
		why      string
	}{
		{
			name:     "片側だけ伏せた値 (#188 の漏洩そのもの)",
			input:    `<p:Owner>scrubbed-1\someuser</p:Owner>`,
			wantFail: true,
			why:      "ホスト名側だけ伏せてユーザー名が生で残っている形",
		},
		{
			name:     "両側とも伏せ字",
			input:    `<p:Owner>scrubbed-1\scrubbed-8</p:Owner>`,
			wantFail: false,
			why:      "正しく伏せられている",
		},
		{
			name:     "Go の interpreted string (バックスラッシュが 2 個)",
			input:    `want := "scrubbed-1\\scrubbed-8"`,
			wantFail: false,
			why:      "ソース上は \\ が 2 個になる。ここを弾くと正規表現を緩める圧力になる",
		},
		{
			name:     "XML エスケープした伏せ字表記",
			input:    `scrubbed-1\&lt;user&gt;`,
			wantFail: false,
			why:      "伏せ字プレースホルダ",
		},
		{
			name:     "既知の実環境識別子 (ホスト名)",
			input:    "host is DESKTOP-KAKEF4K here",
			wantFail: true,
			why:      "denylist",
		},
		{
			name:     "既知の実環境識別子 (VM 名)",
			input:    "node k8s-worker-01 failed",
			wantFail: true,
			why:      "denylist",
		},
		{
			name:     "伏せ字だけ (後ろに何も無い)",
			input:    "scrubbed-1 と scrubbed-2 を使う",
			wantFail: false,
			why:      "バックスラッシュが無いので候補にならない",
		},
		{
			// 🔴 **これは検出する (保守的)。** `scrubbed-3\file.txt` は
			// 「伏せ字 + バックスラッシュ + 生の値」と**構造的に区別できない**。
			// VM 名を伏せたパスの次の要素が実ファイル名である可能性がある
			// (`...\scrubbed-3\<実 VM 名>.vhdx` 等)。
			// 人が中身を見て判断する方に倒す。
			name:     "伏せ字の直後にバックスラッシュが続くパス",
			input:    `C:\Users\scrubbed-3\file.txt`,
			wantFail: true,
			why:      "片側だけ伏せた値と構造的に区別できないので検出する",
		},
		{
			name:     "伏せ字がパスの末尾 (後ろにバックスラッシュが無い)",
			input:    `C:\Users\scrubbed-3`,
			wantFail: false,
			why:      "候補にならない",
		},
		{
			name:     "何も無いテキスト",
			input:    "ただの説明文",
			wantFail: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out := runScanText(t, tt.input)
			if code == 2 {
				t.Fatalf("検査が成立しなかった (rc=2): %s", out)
			}
			gotFail := code != 0
			if gotFail != tt.wantFail {
				t.Errorf("rc=%d (検出=%v), want 検出=%v。%s\n出力: %s",
					code, gotFail, tt.wantFail, tt.why, out)
			}
			// 🔴 **検出した値を出力してはいけない** (CI ログでの二次公開)。
			if gotFail && strings.Contains(out, "someuser") {
				t.Errorf("検出した値が出力に含まれている (二次公開):\n%s", out)
			}
		})
	}
}

// TestScanForbiddenScript_RejectsBadMode は使い方を誤ったときに
// 「問題なし」ではなく **検査不成立 (rc=2)** で落ちることを検証する。
//
// ここが rc=0 だと、ワークフローの書き間違いで検査が静かに消える。
func TestScanForbiddenScript_RejectsBadMode(t *testing.T) {
	abs, err := filepath.Abs(scanForbiddenScript)
	if err != nil {
		t.Fatalf("パスを解決できない: %v", err)
	}
	for _, mode := range []string{"", "bogus"} {
		args := []string{abs}
		if mode != "" {
			args = append(args, mode)
		}
		cmd := exec.Command("bash", args...) //#nosec G204 -- テスト内の固定パス
		out, runErr := cmd.CombinedOutput()
		var ee *exec.ExitError
		if runErr == nil || !asExitError(runErr, &ee) {
			t.Errorf("mode=%q でエラーにならなかった: %s", mode, out)
			continue
		}
		if ee.ExitCode() != 2 {
			t.Errorf("mode=%q の rc=%d, want 2 (検査不成立)。出力: %s", mode, ee.ExitCode(), out)
		}
	}
}
