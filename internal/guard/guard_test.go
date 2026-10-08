// Package guard は「テストの fixture がどこから来たか」を機械的に守る関所 (#157)。
//
// このリポジトリは「手書き golden が実機に無い挙動を仕様として固定し、ユニットテストは
// 全緑なのに実機で落ちる」事故を 7 回繰り返している。**警告文と規約は 1 度も効かなかった**
// (5 回目は「4 回繰り返している」と CLAUDE.md に書いた同じセッションが数時間後に起こした)。
//
// 「実機から採取したと書いてあるか」は機械検証できない (主張の真偽は判定できない)。
// 検証できるのは証跡の有無だけなので、
//
//   - 記録器が書いた印と本文の sha256 を持つファイルだけを「実機由来」として扱う
//   - 合成は testdata/synthetic/ + derived-from: に限る
//   - テストソースに応答 XML を直接書かせない (testdata への関所の迂回路)
//
// の 3 つを機械で確かめる。
//
// **これは万能ではない。** 印も sha256 も自分で計算して貼れるので、意図的な偽造は止まらない。
// 止まるのは「それらしい XML を思いつきで書く」経路と「記録した後で値を調整する」経路で、
// 過去 7 件はすべてこの 2 つ。摩擦を上げる仕組みであって、証明ではない。
//
// これらは既に必須の "Test / Unit Tests" ジョブで走るので、CI 側の追加配線は要らない。
package guard

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/r4sd/go-wsman/wsman"
)

// repoRoot はこのパッケージから見たリポジトリのルート。
const repoRoot = "../.."

// legacyGoldens は本関所の導入より前からある XML fixture。
//
// 記録器の印を持たないので**新規追加は許さないが、既存は落とさない**。
// 棚卸しを独立した作業として積むと重くて進まないので、**触ったときに記録し直して
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
	"hyperv/testdata/invoke_response_define_system.xml":             {},
	"hyperv/testdata/invoke_response_destroy_snapshot.xml":          {},
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

// TestFixturesAreRecordedOrDerived は fixture が実機の記録か、実機の記録からの派生かを確かめる。
func TestFixturesAreRecordedOrDerived(t *testing.T) {
	seen := make(map[string]bool, len(legacyGoldens))

	for _, dir := range []string{"wsman/testdata", "hyperv/testdata"} {
		root := filepath.Join(repoRoot, dir)
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !fixtureExts[filepath.Ext(path)] {
				return err
			}
			rel := relPath(path)
			content, readErr := os.ReadFile(path) //#nosec G304 -- testdata の走査
			if readErr != nil {
				t.Errorf("%s: 読めない: %v", rel, readErr)
				return nil
			}
			// MOF fixture は CIM 仕様の抜き書きで、実機の応答ではないので記録を求めない。
			// ただし「mof/ に置けば何でも通る」にはしない (免除がそのまま迂回路になる)。
			if strings.Contains(rel, "/mof/") {
				if rawXMLInSource.Match(content) {
					t.Errorf("%s: MOF fixture に応答 XML が入っている。実機の記録として testdata/ へ置くこと", rel)
				}
				return nil
			}
			if _, ok := legacyGoldens[rel]; ok {
				seen[rel] = true
				return nil
			}
			if strings.Contains(rel, "/synthetic/") {
				checkDerivedFrom(t, content, rel)
				return nil
			}
			if err := wsman.VerifyRecordedHash(content); err != nil {
				t.Errorf("%s: %v\n"+
					"  実機の応答が要るなら記録する (WSMAN_RECORD_DIR を設定して統合テストを実行)。\n"+
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

// selfTestMarker は「記録器そのものを検査するための最小データ」の印。
//
// 匿名化の実装を試すための合成データは、実機の応答の派生ではない。そこを
// 無理に derived-from で繋ぐと、**関所を通すためにポインタを付け替える**ことになり、
// 「主張ではなく証跡を見る」という趣旨を自分で破る (実際に一度やった)。
//
// 代わりに明示カテゴリを 1 つ用意し、**CIM クラス名を含まないこと**を条件にする。
// クラス名を持てない以上、実機の挙動を仕様として固定することはできない。
const selfTestMarker = "purpose: recorder-self-test"

// cimClassPattern は fixture に現れる CIM クラス名。
var cimClassPattern = regexp.MustCompile(`(?:Msvm|CIM|Win32)_[A-Za-z0-9]+`)

// checkDerivedFrom は合成 fixture の来歴を確かめる。
func checkDerivedFrom(t *testing.T, content []byte, rel string) {
	t.Helper()

	if strings.Contains(string(content), selfTestMarker) {
		if cls := cimClassPattern.FindAllString(string(content), -1); len(cls) > 0 {
			t.Errorf("%s: %s を名乗る fixture が CIM クラス名 (%s) を含んでいる。\n"+
				"  実機の挙動に関わるものは記録するか、実機の記録からの派生にすること。",
				rel, selfTestMarker, strings.Join(uniq(cls), ", "))
		}
		// クラス名が無くても、応答の骨格を持つものは実機の挙動を主張できてしまう
		// (Fault の形、Pull の終端条件など)。自己検査用の免除はそこまで広げない。
		for _, shape := range []string{"Fault", "PullResponse", "EnumerateResponse", "Items"} {
			if strings.Contains(string(content), shape) {
				t.Errorf("%s: %s を名乗る fixture が応答の骨格 (%s) を含んでいる。\n"+
					"  この免除は記録器そのものの検査だけに使うこと。", rel, selfTestMarker, shape)
			}
		}
		return
	}

	m := regexp.MustCompile(`derived-from:\s*(\S+)`).FindSubmatch(content)
	if m == nil {
		t.Errorf("%s: 合成 fixture には derived-from: <派生元のパス> が要る (記録器の自己検査用なら %q)", rel, selfTestMarker)
		return
	}
	origin := string(m[1])
	originPath := filepath.Join(repoRoot, origin)
	originContent, err := os.ReadFile(originPath) //#nosec G304 -- testdata 内の参照先
	if err != nil {
		t.Errorf("%s: derived-from が指す %q が存在しない", rel, origin)
		return
	}
	if !fixtureExts[filepath.Ext(origin)] || !strings.Contains(origin, "/testdata/") {
		t.Errorf("%s: derived-from が fixture 以外 (%q) を指している", rel, origin)
		return
	}
	// 派生元は**実機の記録**でなければならない。legacy を指せると、印と sha256 を回避する
	// 最安の抜け道が synthetic/ に移るだけになる。クラスごとに最低 1 回は
	// 実機から録ることを強制する。
	if err := wsman.VerifyRecordedHash(originContent); err != nil {
		t.Errorf("%s: derived-from が指す %q が実機の記録ではない (%v)。\n"+
			"  合成は実機から録ったものの派生でなければならない。まず対象クラスを記録すること。", rel, origin, err)
		return
	}
	// **派生元は本当に派生元か。** 合成が扱う CIM クラスが派生元に無いなら、
	// それは派生ではなく「関所を通すために付けたポインタ」。実際に一度やった。
	originClasses := make(map[string]struct{})
	for _, c := range cimClassPattern.FindAllString(string(originContent), -1) {
		originClasses[c] = struct{}{}
	}
	var missing []string
	for _, c := range uniq(cimClassPattern.FindAllString(string(content), -1)) {
		if _, ok := originClasses[c]; !ok {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%s: 扱っている CIM クラス %s が派生元 %q に無い。\n"+
			"  そのクラスを実機から記録してから派生させること。", rel, strings.Join(missing, ", "), origin)
	}
}

// uniq は重複を除いた文字列スライスを返す。
func uniq(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
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
				"  記録した fixture を loadGolden で読むか、合成なら testdata/synthetic/ に置いて derived-from: を書く。", rel, n)
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

// macInElementPattern は fixture 中の MAC アドレス値。
var macInElementPattern = regexp.MustCompile(`<(?:[A-Za-z0-9]+:)?(?:PermanentAddress|Address)(?:\s[^>]*)?>([0-9A-Fa-f]{12})</`)

// macLiteralPattern はソース中に直接書かれた MAC。区切り付き・無しの両方。
var macLiteralPattern = regexp.MustCompile(`\b(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}\b|"[0-9A-Fa-f]{12}"`)

// guidPattern は任意の GUID。
var guidPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)

// recordedGUIDPlaceholder は記録器が GUID を置き換えた後の形。
//
// wsman/record.go の placeholderFor が `00000000-0000-4000-8000-%012x` を返す。
// ここを変えたら record.go 側と一緒に直す (片方だけ変えるとこの関所が空振りする)。
var recordedGUIDPlaceholder = regexp.MustCompile(`(?i)^00000000-0000-4000-8000-[0-9a-f]{12}$`)

// allowedMACPrefixes は書いてよい MAC の接頭辞。
//
//   - 00155D: Hyper-V が仮想 NIC に振る OUI。記録器のプレースホルダがこれを使う
//   - 00005E0053: RFC 7042 の文書用アドレス
var allowedMACPrefixes = []string{"00155D", "00005E0053"}

// TestNoRealMACAddresses は実環境の MAC がリポジトリに入るのを止める (#157)。
//
// CI の no-private-addresses は IP しか見ない。実際に、実機 NIC の MAC を実機の記録に
// 1 回、テストソースに 1 回書いている (プライベート IP も同型で 1 回)。
// 「実機で見た値をそのまま書き写す」型なので、機械で止める。
func TestNoRealMACAddresses(t *testing.T) {
	allowed := func(mac string) bool {
		norm := strings.ToUpper(strings.NewReplacer(":", "", "-", "", `"`, "").Replace(mac))
		for _, p := range allowedMACPrefixes {
			if strings.HasPrefix(norm, p) {
				return true
			}
		}
		return false
	}

	for _, dir := range []string{"wsman", "hyperv"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			ext := filepath.Ext(path)
			if ext != ".go" && !fixtureExts[ext] {
				return nil
			}
			content, readErr := os.ReadFile(path) //#nosec G304 -- リポジトリ内の走査
			if readErr != nil {
				t.Errorf("%s: 読めない: %v", relPath(path), readErr)
				return nil
			}
			var found []string
			for _, m := range macInElementPattern.FindAllStringSubmatch(string(content), -1) {
				if !allowed(m[1]) {
					found = append(found, m[1])
				}
			}
			if ext == ".go" {
				for _, m := range macLiteralPattern.FindAllString(string(content), -1) {
					if !allowed(m) {
						found = append(found, m)
					}
				}
			}
			if len(found) > 0 {
				t.Errorf("%s: 実環境の MAC と思われる値がある (%s)。\n"+
					"  実機の記録なら録り直す。テストなら RFC 7042 の文書用アドレス (00-00-5E-00-53-xx) を使う。",
					relPath(path), strings.Join(uniq(found), ", "))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("%s の走査に失敗: %v", dir, err)
		}
	}
}

// TestRecordedFixturesOnlyHavePlaceholderGUIDs は記録 fixture と、そこから派生した
// 合成 fixture の GUID が**すべて記録器のプレースホルダ形**であることを検証する。
//
// # なぜ必要か
//
// 記録器は実機の GUID をプレースホルダへ置換する。置換が漏れると
// **実環境の VM / NIC / Job の GUID が公開リポジトリに入る** (#154 / #188 の型)。
//
// CI の denylist (`.github/scripts/scan_forbidden.sh`) は**既知の値の再混入しか
// 止められない**。こちらは形で見るので、denylist に無い値でも捕まえる。
//
// ⚠️ **実効範囲は狭い。** 捕まえられるのは
// 「記録器が認識する形 (ハイフン付き GUID) の値が、記録由来のファイルに入っている」
// 場合だけ。記録器は該当する GUID を全部置換するので、ここが落ちる経路は実質
//
//	a) 記録器が退行してハイフン付き GUID を置換しなくなった
//	b) 記録ファイルを手で書き換えて sha256 も再計算した
//
// の 2 つ。次のものは**素通りする**:
//
//	ハイフン無しの 32 hex GUID (記録器の正規表現も同じ盲点)
//	legacyGoldens (手書き golden は対象外)
//	testdata/synthetic/ 以外に置いた合成
//	record.go 側のプレースホルダ形だけを変えた場合 (既存 fixture は古い形のまま緑)
//
// # 対象を記録由来のファイルに限る理由
//
// 手書き golden は合成 GUID を大量に持っており (`AAAAAAAA-1111-...` 等)、
// 形で一律に弾くと誤検知だらけになる。**記録由来のファイルだけは
// 「置換後の形しか入っていない」という強い不変条件が成立する**ので、そこに絞る。
func TestRecordedFixturesOnlyHavePlaceholderGUIDs(t *testing.T) {
	scanned, guidsSeen := 0, 0

	for _, dir := range []string{"wsman", "hyperv"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !fixtureExts[filepath.Ext(path)] {
				return err
			}
			content, readErr := os.ReadFile(path) //#nosec G304 -- リポジトリ内の走査
			if readErr != nil {
				t.Errorf("%s: 読めない: %v", relPath(path), readErr)
				return nil
			}
			text := string(content)
			// 記録器が書いたもの、またはそこから派生した合成のみ対象。
			//
			// ⚠️ 合成の判定に `strings.Contains(text, "derived-from:")` を使ってはいけない。
			// **記録器のヘッダ定型文にその文字列が入っている**ため、記録ファイル全件に
			// 真になり、2 つの分岐が区別できなくなる (ラベルが嘘になる)。
			// パスで判定する (関所は合成を testdata/synthetic/ に置くことを強制している)。
			recorded := wsman.VerifyRecordedHash(content) == nil
			derived := strings.Contains(filepath.ToSlash(path), "/testdata/synthetic/")
			if !recorded && !derived {
				return nil
			}
			scanned++

			var bad []string
			for _, g := range guidPattern.FindAllString(text, -1) {
				guidsSeen++
				if !recordedGUIDPlaceholder.MatchString(g) {
					bad = append(bad, g)
				}
			}
			if len(bad) > 0 {
				kind := "記録"
				if !recorded {
					kind = "記録から派生した合成"
				}
				t.Errorf("%s (%s) にプレースホルダ形でない GUID がある (%s)。\n"+
					"  記録器の伏せ字が漏れている可能性がある。録り直すか、"+
					"合成なら値をプレースホルダ形に直すこと (#154)。",
					relPath(path), kind, strings.Join(uniq(bad), ", "))
			}
			return nil
		})
		if err != nil {
			t.Errorf("%s の走査に失敗: %v", dir, err)
		}
	}

	// 🔴 **空振り検出。** パスが変わった・印の文字列が変わった場合、
	// 対象 0 件でも緑になってしまう。記録 fixture は現に存在するので下限を置く。
	if scanned < 10 {
		t.Errorf("走査した記録由来の fixture が %d 件しかない。"+
			"VerifyRecordedHash / derived-from の判定か走査パスが変わって"+
			"空振りしている可能性がある", scanned)
	}
	if guidsSeen == 0 {
		t.Errorf("GUID を 1 つも見ていない。guidPattern が壊れている可能性がある")
	}
	t.Logf("記録由来 %d 件 / GUID %d 個を検査した", scanned, guidsSeen)
}
