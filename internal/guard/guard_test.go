// Package guard は「テストの fixture がどこから来たか」を機械的に守る関所 (#157)。
//
// このリポジトリは「手書き golden が実機に無い挙動を仕様として固定し、ユニットテストは
// 全緑なのに実機で落ちる」事故を 7 回繰り返している。**警告文と規約は 1 度も効かなかった**
// (5 回目は「4 回繰り返している」と CLAUDE.md に書いた同じセッションが数時間後に起こした)。
//
// 一方 #138 で Unmarshal を削除して**選択肢自体を消した**型の事故は再発していない。
// ここも同じ方針を取る。「実機から採取したと書いてあるか」は検証できない (主張の真偽は
// 機械判定できない) ので、**そもそも手で書いた fixture を置けなくする**。
//
// これらは既に必須の "Test / Unit Tests" ジョブで走るので、CI 側の追加配線は要らない。
package guard

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot はこのパッケージから見たリポジトリのルート。
const repoRoot = "../.."

// legacyGoldens は本関所の導入より前からある XML fixture。
//
// 来歴が確認できていないので**新規追加は許さないが、既存は落とさない**。
// 棚卸しを独立した作業として積むと重くて進まないので、**触ったときに録音し直して
// このリストから外す**運用にする。リストが縮むことが進捗。
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
	"wsman/testdata/pull_response_xsinil_synthetic.xml":             {},
	"wsman/testdata/put_request_service.xml":                        {},
	"wsman/testdata/put_response_service.xml":                       {},
}

// legacyRawXMLTests は本関所の導入より前から、テストソース中に生の XML を持つファイル。
//
// testdata への関所だけ作っても、テストの中に直接 XML を書けば迂回できる。
// 迂回路なのでここが最優先だが、既存分は同じく「触ったら外す」で縮める。
var legacyRawXMLTests = map[string]struct{}{
	"hyperv/client_test.go":            {},
	"hyperv/embedded_test.go":          {},
	"hyperv/firmware_test.go":          {},
	"hyperv/guest_network_test.go":     {},
	"hyperv/integration_unit_test.go":  {},
	"hyperv/resource_settings_test.go": {},
	"hyperv/vm_resources_test.go":      {},
	"hyperv/vm_test.go":                {},
	"wsman/insecure_test.go":           {},
	"wsman/invoke_test.go":             {},
	"wsman/transport_test.go":          {},
}

// rawXMLInSource はテストソースに直接書かれた応答 XML を検出する。
// SOAP エンベロープと CIM のクラス要素・INSTANCE を見る。
var rawXMLInSource = regexp.MustCompile(`<s:Envelope|<p:Msvm_|<INSTANCE CLASSNAME`)

// TestNoHandWrittenGoldens は新しい XML fixture が手で追加されていないことを確認する。
//
// 実機の応答が要るなら録音する (WSMAN_RECORD_DIR を設定して統合テストを回すと
// カセットが貯まる。再生は wsman.NewReplayClient)。
// 合成データが要るなら testdata/synthetic/ 配下に置き、どの録音物から派生したかを
// derived-from: で書く。「理由を書く」より強く、参照先の存在を機械で確かめられる。
func TestNoHandWrittenGoldens(t *testing.T) {
	for _, dir := range []string{"wsman/testdata", "hyperv/testdata"} {
		root := filepath.Join(repoRoot, dir)
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".xml") {
				return err
			}
			rel := filepath.ToSlash(strings.TrimPrefix(filepath.Clean(path), filepath.Clean(repoRoot)+"/"))
			if _, ok := legacyGoldens[rel]; ok {
				return nil
			}
			if strings.Contains(rel, "/synthetic/") {
				return checkDerivedFrom(t, path, rel)
			}
			t.Errorf("%s: 新しい XML fixture を手で追加できない。\n"+
				"  実機の応答が要るなら録音する (WSMAN_RECORD_DIR を設定して統合テストを実行 → wsman.NewReplayClient で再生)。\n"+
				"  合成データが要るなら testdata/synthetic/ に置き、derived-from: でどの録音物から派生したかを書く。", rel)
			return nil
		})
		if err != nil {
			t.Fatalf("%s の走査に失敗: %v", dir, err)
		}
	}
}

// checkDerivedFrom は合成 fixture が実在する録音物から派生していることを確かめる。
func checkDerivedFrom(t *testing.T, path, rel string) error {
	t.Helper()
	content, err := os.ReadFile(path) //#nosec G304 -- testdata の走査
	if err != nil {
		t.Errorf("%s: 読めない: %v", rel, err)
		return nil
	}
	m := regexp.MustCompile(`derived-from:\s*(\S+)`).FindSubmatch(content)
	if m == nil {
		t.Errorf("%s: 合成 fixture には derived-from: <派生元のパス> が要る", rel)
		return nil
	}
	origin := filepath.Join(repoRoot, string(m[1]))
	if _, err := os.Stat(origin); err != nil {
		t.Errorf("%s: derived-from が指す %q が存在しない", rel, m[1])
	}
	return nil
}

// TestNoRawXMLInTestSources はテストソースに応答 XML を直接書くことを禁じる。
//
// testdata への関所があっても、テストの中に文字列で書けば迂回できる。
// 実際に導入時点で 11 ファイルが該当していた。
func TestNoRawXMLInTestSources(t *testing.T) {
	for _, dir := range []string{"wsman", "hyperv"} {
		matches, err := filepath.Glob(filepath.Join(repoRoot, dir, "*_test.go"))
		if err != nil {
			t.Fatalf("%s の走査に失敗: %v", dir, err)
		}
		for _, path := range matches {
			rel := dir + "/" + filepath.Base(path)
			if _, ok := legacyRawXMLTests[rel]; ok {
				continue
			}
			content, err := os.ReadFile(path) //#nosec G304 -- テストソースの走査
			if err != nil {
				t.Errorf("%s: 読めない: %v", rel, err)
				continue
			}
			if rawXMLInSource.Match(content) {
				t.Errorf("%s: テストソースに応答 XML を直接書かない。\n"+
					"  録音したカセットを wsman.NewReplayClient で再生するか、"+
					"合成なら testdata/synthetic/ に置いて derived-from: を書く。", rel)
			}
		}
	}
}
