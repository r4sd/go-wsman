// Package guard は「テストの fixture がどこから来たか」を機械的に守る関所 (#157)。
//
// このリポジトリは「手書き golden が実機に無い挙動を仕様として固定し、ユニットテストは
// 全緑なのに実機で落ちる」事故を 7 回繰り返している。**警告文と規約は 1 度も効かなかった**
// (5 回目は「4 回繰り返している」と CLAUDE.md に書いた同じセッションが数時間後に起こした)。
//
// 「実機から採取したと書いてあるか」は機械検証できない (主張の真偽は判定できない)。
// 検証できるのは証跡の有無だけなので、
//
//   - 録音器が書いた印と本文の sha256 を持つファイルだけを「実機由来」として扱う
//   - 合成は testdata/synthetic/ + derived-from: に限る
//   - テストソースに応答 XML を直接書かせない (testdata への関所の迂回路)
//
// の 3 つを機械で確かめる。
//
// **これは万能ではない。** 印も sha256 も自分で計算して貼れるので、意図的な偽造は止まらない。
// 止まるのは「それらしい XML を思いつきで書く」経路と「録音した後で値を調整する」経路で、
// 過去 7 件はすべてこの 2 つ。摩擦を上げる仕組みであって、証明ではない。
//
// これらは既に必須の "Test / Unit Tests" ジョブで走るので、CI 側の追加配線は要らない。
package guard

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/r4sd/go-wsman/wsman"
)

// repoRoot はこのパッケージから見たリポジトリのルート。
const repoRoot = "../.."

// legacyGoldens は本関所の導入より前からある XML fixture。
//
// 録音器の印を持たないので**新規追加は許さないが、既存は落とさない**。
// 棚卸しを独立した作業として積むと重くて進まないので、**触ったときに録音し直して
// このリストから外す**運用にする。リストが縮むことが進捗。
//
// ファイルを消した/改名したのにエントリが残っていると落ちる (幽霊を残さないため)。
var legacyGoldens = map[string]struct{}{
	"hyperv/testdata/enumerate_response_computersystem.xml":         {},
	"hyperv/testdata/enumerate_response_externalethernetport.xml":   {},
	"hyperv/testdata/enumerate_response_idecontroller.xml":          {},
	"hyperv/testdata/enumerate_response_memorysettingdata.xml":      {},
	"hyperv/testdata/enumerate_response_processorsettingdata.xml":   {},
	"hyperv/testdata/enumerate_response_storageallocation.xml":      {},
	"hyperv/testdata/enumerate_response_syntheticethernetport.xml":  {},
	"hyperv/testdata/enumerate_response_systemsettingdata.xml":      {},
	"hyperv/testdata/enumerate_response_virtualethernetswitch.xml":  {},
	"hyperv/testdata/fault_destination_unreachable.xml":             {},
	"hyperv/testdata/get_response_computersystem.xml":               {},
	"hyperv/testdata/get_response_concretejob_completed.xml":        {},
	"hyperv/testdata/get_response_concretejob_exception.xml":        {},
	"hyperv/testdata/get_response_concretejob_running.xml":          {},
	"hyperv/testdata/invoke_response_add_feature_settings.xml":      {},
	"hyperv/testdata/invoke_response_add_resource_settings.xml":     {},
	"hyperv/testdata/invoke_response_apply_snapshot.xml":            {},
	"hyperv/testdata/invoke_response_create_snapshot.xml":           {},
	"hyperv/testdata/invoke_response_create_vhd.xml":                {},
	"hyperv/testdata/invoke_response_define_switch.xml":             {},
	"hyperv/testdata/invoke_response_define_system.xml":             {},
	"hyperv/testdata/invoke_response_destroy_snapshot.xml":          {},
	"hyperv/testdata/invoke_response_destroy_switch.xml":            {},
	"hyperv/testdata/invoke_response_destroy_system.xml":            {},
	"hyperv/testdata/invoke_response_get_vhd.xml":                   {},
	"hyperv/testdata/invoke_response_modify_resource_settings.xml":  {},
	"hyperv/testdata/invoke_response_modify_system_settings.xml":    {},
	"hyperv/testdata/invoke_response_remove_resource_settings.xml":  {},
	"hyperv/testdata/invoke_response_request_state_change.xml":      {},
	"hyperv/testdata/invoke_response_resize_vhd.xml":                {},
	"hyperv/testdata/pull_response_computersystem.xml":              {},
	"hyperv/testdata/pull_response_computersystem_dup.xml":          {},
	"hyperv/testdata/pull_response_diskdrive_mixed.xml":             {},
	"hyperv/testdata/pull_response_dvddrive_mixed.xml":              {},
	"hyperv/testdata/pull_response_ethernetportallocation.xml":      {},
	"hyperv/testdata/pull_response_externalethernetport.xml":        {},
	"hyperv/testdata/pull_response_gpupartition_mixed.xml":          {},
	"hyperv/testdata/pull_response_idecontroller.xml":               {},
	"hyperv/testdata/pull_response_idecontroller_mixed.xml":         {},
	"hyperv/testdata/pull_response_memorysettingdata.xml":           {},
	"hyperv/testdata/pull_response_memorysettingdata_multi.xml":     {},
	"hyperv/testdata/pull_response_memorysettingdata_no_dynmem.xml": {},
	"hyperv/testdata/pull_response_processorsettingdata.xml":        {},
	"hyperv/testdata/pull_response_storageallocation.xml":           {},
	"hyperv/testdata/pull_response_syntheticethernetport.xml":       {},
	"hyperv/testdata/pull_response_syntheticethernetport_multi.xml": {},
	"hyperv/testdata/pull_response_systemsettingdata.xml":           {},
	"hyperv/testdata/pull_response_systemsettingdata_full.xml":      {},
	"hyperv/testdata/pull_response_systemsettingdata_mixed.xml":     {},
	"hyperv/testdata/pull_response_virtualethernetswitch.xml":       {},
	"wsman/testdata/create_response_process.xml":                    {},
	"wsman/testdata/delete_response.xml":                            {},
	"wsman/testdata/enumerate_request_wql.xml":                      {},
	"wsman/testdata/enumerate_response.xml":                         {},
	"wsman/testdata/envelope_empty.xml":                             {},
	"wsman/testdata/envelope_full_headers.xml":                      {},
	"wsman/testdata/envelope_get_action.xml":                        {},
	"wsman/testdata/fault_access_denied.xml":                        {},
	"wsman/testdata/fault_invalid_parameter.xml":                    {},
	"wsman/testdata/get_response_computersystem.xml":                {},
	"wsman/testdata/get_response_vsetting_array.xml":                {},
	"wsman/testdata/invoke_response_returnvalue0.xml":               {},
	"wsman/testdata/invoke_response_with_output.xml":                {},
	"wsman/testdata/pull_response.xml":                              {},
	"wsman/testdata/pull_response_array.xml":                        {},
	"wsman/testdata/pull_response_end.xml":                          {},
	"wsman/testdata/pull_response_xsinil_real.xml":                  {},
	"wsman/testdata/put_request_service.xml":                        {},
	"wsman/testdata/put_response_service.xml":                       {},
}

// legacyRawXML は本関所の導入より前から、ソース中に応答 XML を持つファイルと
// その出現数。
//
// testdata への関所だけ作っても、テストの中に直接 XML を書けば迂回できる。
// **数まで固定するのは「既存ファイルに新しいケースを足して、そこに生 XML を書く」が
// 今後の最有力経路だから**。ファイル単位の許可だけだと、そこが素通りになる。
//
// 数が増えたら落ちる。減ったらリストを更新すること (それが進捗)。
var legacyRawXML = map[string]int{
	"hyperv/client_test.go":            2,
	"hyperv/embedded.go":               4,
	"hyperv/embedded_test.go":          12,
	"hyperv/firmware_test.go":          1,
	"hyperv/guest_network_test.go":     2,
	"hyperv/integration_unit_test.go":  2,
	"hyperv/resource_settings_test.go": 2,
	"hyperv/vm_resources_test.go":      1,
	"hyperv/vm_test.go":                1,
	"wsman/insecure_test.go":           2,
	"wsman/invoke_test.go":             1,
	"wsman/transport_test.go":          1,
}

// rawXMLInSource はソースに直接書かれた応答 XML を検出する。
//
// prefix を固定しない (s: / p: 以外でも書けてしまうため)。
var rawXMLInSource = regexp.MustCompile(
	`<[A-Za-z0-9]{1,8}:Envelope|<[A-Za-z0-9]{1,8}:Msvm_|<INSTANCE CLASSNAME|Msvm_[A-Za-z]+ xmlns`)

// fixtureExts は fixture とみなす拡張子。
var fixtureExts = map[string]bool{".xml": true, ".yaml": true, ".yml": true, ".txt": true}

// TestFixturesAreRecordedOrDerived は fixture が録音物か、録音物からの派生かを確かめる。
func TestFixturesAreRecordedOrDerived(t *testing.T) {
	seen := make(map[string]bool, len(legacyGoldens))

	for _, dir := range []string{"wsman/testdata", "hyperv/testdata"} {
		root := filepath.Join(repoRoot, dir)
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !fixtureExts[filepath.Ext(path)] {
				return err
			}
			rel := relPath(path)
			// MOF fixture は CIM 仕様の抜き書きで、実機の応答ではない (突合の基準として使う)。
			if strings.Contains(rel, "/mof/") {
				return nil
			}
			if _, ok := legacyGoldens[rel]; ok {
				seen[rel] = true
				return nil
			}
			content, readErr := os.ReadFile(path) //#nosec G304 -- testdata の走査
			if readErr != nil {
				t.Errorf("%s: 読めない: %v", rel, readErr)
				return nil
			}
			if strings.Contains(rel, "/synthetic/") {
				checkDerivedFrom(t, content, rel)
				return nil
			}
			if err := wsman.VerifyRecordedHash(content); err != nil {
				t.Errorf("%s: %v\n"+
					"  実機の応答が要るなら録音する (WSMAN_RECORD_DIR を設定して統合テストを実行)。\n"+
					"  合成データが要るなら testdata/synthetic/ に置き、derived-from: で派生元を書く。", rel, err)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("%s の走査に失敗: %v", dir, err)
		}
	}

	// 幽霊エントリを残さない。消した/改名したらリストからも外す。
	for rel := range legacyGoldens {
		if !seen[rel] {
			t.Errorf("legacyGoldens に %s が残っているが、ファイルが無い。リストから外すこと", rel)
		}
	}
}

// checkDerivedFrom は合成 fixture が実在するファイルから派生していることを確かめる。
func checkDerivedFrom(t *testing.T, content []byte, rel string) {
	t.Helper()
	m := regexp.MustCompile(`derived-from:\s*(\S+)`).FindSubmatch(content)
	if m == nil {
		t.Errorf("%s: 合成 fixture には derived-from: <派生元のパス> が要る", rel)
		return
	}
	origin := string(m[1])
	if _, err := os.Stat(filepath.Join(repoRoot, origin)); err != nil {
		t.Errorf("%s: derived-from が指す %q が存在しない", rel, origin)
		return
	}
	if !fixtureExts[filepath.Ext(origin)] || !strings.Contains(origin, "/testdata/") {
		t.Errorf("%s: derived-from が fixture 以外 (%q) を指している", rel, origin)
	}
}

// TestNoRawXMLInSources はソースに応答 XML を直接書くことを禁じる。
//
// testdata への関所があっても、ソースの中に文字列で書けば迂回できる。
// 既存ファイルは出現数まで固定してあるので、**そこへ新しく足す**のも検出される。
func TestNoRawXMLInSources(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join(repoRoot, "*", "*.go"))
	if err != nil {
		t.Fatalf("走査に失敗: %v", err)
	}
	found := make(map[string]int)
	for _, path := range matches {
		rel := relPath(path)
		content, err := os.ReadFile(path) //#nosec G304 -- ソースの走査
		if err != nil {
			t.Errorf("%s: 読めない: %v", rel, err)
			continue
		}
		if n := len(rawXMLInSource.FindAll(content, -1)); n > 0 {
			found[rel] = n
		}
	}

	for rel, n := range found {
		allowed, ok := legacyRawXML[rel]
		switch {
		case !ok:
			t.Errorf("%s: ソースに応答 XML を直接書かない (%d 箇所)。\n"+
				"  録音した fixture を loadGolden で読むか、合成なら testdata/synthetic/ に置いて derived-from: を書く。", rel, n)
		case n > allowed:
			t.Errorf("%s: 生 XML が %d → %d 箇所に増えている。\n"+
				"  既存ファイルへの追記もこの関所の対象。fixture に切り出すこと。", rel, allowed, n)
		}
	}
	for rel, allowed := range legacyRawXML {
		if n := found[rel]; n < allowed {
			t.Errorf("%s: 生 XML が %d → %d 箇所に減った。legacyRawXML を更新すること (これが進捗)", rel, allowed, n)
		}
	}
}

// relPath はリポジトリルートからの相対パスを "/" 区切りで返す。
func relPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
