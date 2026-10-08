package guard_test

import (
	"os"
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

// 🔴 **テストの入力は実行時に連結して組む。**
//
// 禁止値をソースに literal で書くと、**走査スクリプトが自分のテストファイルを検出する**
// (実際に CI が落ちた)。除外リストに足して逃げると、その後そのファイルに
// 本物が混入しても誰も気付かなくなる。連結にすれば
// ソース上は `"DESKTOP-" + "KAKEF4K"` のようにパターンに当たらない形で済み、
// **検査対象から外さずに**負のテストが書ける。
const (
	bs = `\\` // バックスラッシュ 1 個
)

// forbiddenHostName / forbiddenVMName は denylist に実在する値。
// 連結して組むので、このファイル自体はパターンに当たらない。
func forbiddenHostName() string { return "DESKTOP-" + "KAKEF4K" }
func forbiddenVMName() string   { return "k8s-worker-" + "01" }

// partiallyScrubbed は「ホスト名側だけ伏せてユーザー名が生で残った」形 (#188 の漏洩)。
func partiallyScrubbed() string { return "scrubbed-1" + bs + "someuser" }

// bothScrubbed は正しく両側伏せた形。
func bothScrubbed() string { return "scrubbed-1" + bs + "scrubbed-8" }

// goInterpretedString は Go の interpreted string で書かれた場合
// (ソース上はバックスラッシュが 2 個になる)。
func goInterpretedString() string { return "scrubbed-1" + bs + bs + "scrubbed-8" }

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
			input:    "<p:Owner>" + partiallyScrubbed() + "</p:Owner>",
			wantFail: true,
			why:      "ホスト名側だけ伏せてユーザー名が生で残っている形",
		},
		{
			name:     "両側とも伏せ字",
			input:    "<p:Owner>" + bothScrubbed() + "</p:Owner>",
			wantFail: false,
			why:      "正しく伏せられている",
		},
		{
			name:     "Go の interpreted string (バックスラッシュが 2 個)",
			input:    `want := "` + goInterpretedString() + `"`,
			wantFail: false,
			why:      "ソース上は \\ が 2 個になる。ここを弾くと正規表現を緩める圧力になる",
		},
		{
			name:     "XML エスケープした伏せ字表記",
			input:    "scrubbed-1" + bs + "&lt;user&gt;",
			wantFail: false,
			why:      "伏せ字プレースホルダ",
		},
		{
			name:     "既知の実環境識別子 (ホスト名)",
			input:    "host is " + forbiddenHostName() + " here",
			wantFail: true,
			why:      "denylist",
		},
		{
			name:     "既知の実環境識別子 (VM 名)",
			input:    "node " + forbiddenVMName() + " failed",
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
			// 🔴 **これは検出する (保守的)。** 「伏せ字 + バックスラッシュ + 生の値」と
			// **構造的に区別できない**。VM 名を伏せたパスの次の要素が実ファイル名である
			// 可能性がある。人が中身を見て判断する方に倒す。
			name:     "伏せ字の直後にバックスラッシュが続くパス",
			input:    "C:" + bs + "Users" + bs + "scrubbed-3" + bs + "file.txt",
			wantFail: true,
			why:      "片側だけ伏せた値と構造的に区別できないので検出する",
		},
		{
			name:     "伏せ字がパスの末尾 (後ろにバックスラッシュが無い)",
			input:    "C:" + bs + "Users" + bs + "scrubbed-3",
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
			if gotFail && strings.Contains(out, tt.input) {
				t.Errorf("検出した値が出力に含まれている (二次公開):\n%s", out)
			}
		})
	}
}

// TestScanForbiddenScript_TestFileIsNotExcluded はこのテストファイル自身が
// 走査の対象に入っていることを確かめる。
//
// 禁止値を literal で書いて除外リストに足す、という逃げ方をすると、
// **その後このファイルに本物が混入しても誰も気付かない。**
// 入力を連結で組むことで除外が不要になっているので、そこを固定する。
func TestScanForbiddenScript_TestFileIsNotExcluded(t *testing.T) {
	data, err := os.ReadFile("scan_forbidden_script_test.go")
	if err != nil {
		t.Fatalf("自分自身を読めない: %v", err)
	}
	text := string(data)
	// 連結で組んでいるので、ソースには禁止値の literal が無いはず。
	for _, bad := range []string{forbiddenHostName(), forbiddenVMName(), partiallyScrubbed()} {
		if strings.Contains(text, bad) {
			t.Errorf("テストファイルに禁止値が literal で入っている。" +
				"連結で組むか、走査スクリプトの除外が必要になる")
			break
		}
	}
	// スクリプト側の除外にこのファイルが入っていないこと。
	script, err := os.ReadFile(scanForbiddenScript)
	if err != nil {
		t.Fatalf("スクリプトを読めない: %v", err)
	}
	if strings.Contains(string(script), "scan_forbidden_script_test.go") {
		t.Error("走査スクリプトがこのテストファイルを除外している。" +
			"除外すると本物の混入を見逃す")
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
