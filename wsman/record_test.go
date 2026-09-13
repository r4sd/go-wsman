package wsman

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// guidPattern は録音結果に実環境の GUID が残っていないか調べるための検査用パターン。
var guidPattern = regexp.MustCompile(`[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}`)

// recordOnce は httptest サーバへ 1 往復して録音し、書き出されたファイルを返す。
func recordOnce(t *testing.T, respBody []byte, opts ...ClientOption) []string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write(respBody)
	}))
	defer server.Close()

	dir := t.TempDir()
	all := append([]ClientOption{WithRecorder(dir, "probe")}, opts...)
	client, err := NewClient(server.URL, all...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	// 応答のパース結果はここでは問わない。録音は RoundTripper 層で行うため、
	// 上位がエラーを返しても記録される。
	_, _ = client.Enumerate(context.Background(), "http://example.invalid/Msvm_Test")
	if err := client.StopRecording(); err != nil {
		t.Fatalf("StopRecording: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.xml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("録音ファイルが書かれていない (%v)", err)
	}
	return files
}

// recordedFile は検証用に、正しいヘッダを持つ録音ファイルの中身を組み立てる。
func recordedFile(body string) []byte {
	sum := sha256.Sum256([]byte(body))
	return fmt.Appendf(nil, "<!--\n  %s\n  sha256: %s\n-->\n%s",
		recordedByMarker, hex.EncodeToString(sum[:]), body)
}

// TestWithRecorder は実機応答を匿名化した XML として書き出せることを検証する (#157)。
//
// golden を人が書く工程を無くすのが目的なので、「録音できる」だけでなく
// 「そのまま公開リポジトリに置ける状態で落ちる」ところまでを 1 単位とする。
func TestWithRecorder(t *testing.T) {
	body := loadGolden(t, "pull_response_xsinil_real.xml")
	files := recordOnce(t, body)

	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("読めない: %v", err)
	}
	got := string(raw)

	if !strings.Contains(got, "Msvm_ResourceAllocationSettingData") {
		t.Errorf("応答本文が記録されていない:\n%s", got)
	}
	if !strings.Contains(got, recordedByMarker) {
		t.Error("録音器の印が無い")
	}
	if err := VerifyRecordedHash(raw); err != nil {
		t.Errorf("書き出した直後なのにハッシュ検証に失敗: %v", err)
	}

	// 匿名化: 元の応答に含まれる GUID がそのまま残っていないこと。
	for _, guid := range guidPattern.FindAllString(string(body), -1) {
		if strings.Contains(got, guid) {
			t.Errorf("GUID %q が匿名化されずに残っている", guid)
		}
	}
	if len(regexp.MustCompile(`00000000-0000-4000-8000-[0-9a-f]{12}`).FindAllString(got, -1)) == 0 {
		t.Error("プレースホルダ GUID が 1 つも無い。匿名化が働いていない可能性")
	}
}

// TestVerifyRecordedHash_DetectsEdit は録音後の手直しをハッシュが捕まえることを確認する。
//
// 「録音したが後からアサーションに合わせて値を調整する」改変が実際に起きているので、
// そこを検出できることがこの仕組みの要。
func TestVerifyRecordedHash_DetectsEdit(t *testing.T) {
	files := recordOnce(t, loadGolden(t, "pull_response_xsinil_real.xml"))
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("読めない: %v", err)
	}
	edited := strings.Replace(string(raw), "Msvm_ResourceAllocationSettingData", "Msvm_Tampered", 1)
	if err := VerifyRecordedHash([]byte(edited)); err == nil {
		t.Error("録音後の編集を見逃した")
	}
	if err := VerifyRecordedHash([]byte("ヘッダの無いファイル")); err == nil {
		t.Error("録音器の印が無いファイルを通した")
	}
}

// TestWithRecorderScrub は、パターンでは拾えない任意の識別子 (VM 表示名・
// コンピュータ名など) を伏せられること、表記ゆれも取りこぼさないことを検証する。
//
// 匿名化は「したつもり」が最も危ない。実機で録ったものをそのまま公開リポジトリへ
// 置く前提なので、漏れは静かに通さず fail-loud にする。
func TestWithRecorderScrub(t *testing.T) {
	// プライベート IP をソースにリテラルで書かない。この検査自体がプライベート IP を
	// 必要とするが、リテラルで置くと CI の no-private-addresses に引っかかるうえ、
	// 実環境のアドレスを書き写す事故 (実際に一度やった) の入口になる。
	privateIP := net.IPv4(172, 20, 0, 5).String()
	const vmName = "R&D-vm"
	const hostName = "hv01"
	// 実機は XML エスケープして返し、大文字小文字も揃わない。その形を合成 fixture に持つ
	// (テストソースに XML を直接書かない — 関所が禁じている迂回路なので)。
	body := []byte(strings.ReplaceAll(
		string(loadGolden(t, "synthetic/scrub_probe.xml")), "__PRIVATE_IP__", privateIP))

	files := recordOnce(t, body, WithRecorderScrub(vmName, hostName))
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("読めない: %v", err)
	}
	got := string(raw)

	if strings.Contains(got, "R&amp;D-vm") {
		t.Errorf("XML エスケープされた VM 名が残っている:\n%s", got)
	}
	if strings.Contains(strings.ToLower(got), "hv01") {
		t.Errorf("大文字のホスト名が残っている:\n%s", got)
	}
	if strings.Contains(got, privateIP) {
		t.Error("プライベート IP が残っている")
	}
}

// TestVerifyRecordedCatchesLeak は保存後の検証が実際に漏れを捕まえることを確認する。
// 匿名化の実装を信じずに、出力そのものを検査していることの証明。
func TestVerifyRecordedCatchesLeak(t *testing.T) {
	leaked := net.IPv4(192, 168, 1, 10).String()

	if err := verifyRecorded(recordedFile("<probe>"+leaked+"</probe>"), nil, ""); err == nil {
		t.Error("プライベート IP を見逃した")
	} else if !strings.Contains(err.Error(), leaked) {
		t.Errorf("エラーが漏れた値を示していない: %v", err)
	}

	// 指定した名前が XML エスケープ後の形で残っていても捕まえる。
	if err := verifyRecorded(recordedFile("<probe>R&amp;D-vm</probe>"), []string{"R&D-vm"}, ""); err == nil {
		t.Error("エスケープされた名前の残留を見逃した")
	}

	// 大文字小文字が違っても捕まえる。
	if err := verifyRecorded(recordedFile("<probe>HV01</probe>"), []string{"hv01"}, ""); err == nil {
		t.Error("大文字小文字違いの残留を見逃した")
	}

	// 伏せ損ねた MAC も捕まえる。実機 NIC の MAC は OUI からベンダが割れるので、
	// 公開リポジトリへ出す前にここで止める (実際に一度コミットしてしまった)。
	if err := verifyRecorded(recordedFile("<p:PermanentAddress>00005E005301</p:PermanentAddress>"), nil, ""); err == nil {
		t.Error("伏せられていない MAC を見逃した")
	}
	if err := verifyRecorded(recordedFile("<p:PermanentAddress>00155D000001</p:PermanentAddress>"), nil, ""); err != nil {
		t.Errorf("伏せ済みの MAC を誤って拒否した: %v", err)
	}

	if err := verifyRecorded(recordedFile("<probe>clean</probe>"), nil, ""); err != nil {
		t.Errorf("匿名化済みの内容を誤って拒否した: %v", err)
	}
}

// TestWithRecorder_NotEnabled は WithRecorder を渡していない Client で
// StopRecording を呼んでも安全に no-op になることを確認する。
func TestWithRecorder_NotEnabled(t *testing.T) {
	client, err := NewClient("https://example.invalid:5986/wsman")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.StopRecording(); err != nil {
		t.Errorf("録音していない Client の StopRecording はエラーにしない: %v", err)
	}
}

// TestWithRecorder_PooledClientRejected はプール経由の録音が静かに空振らないことを確認する。
// プール内で作られる接続は誰も StopRecording しないため、録音したつもりで 0 件になる。
func TestWithRecorder_PooledClientRejected(t *testing.T) {
	_, err := NewPooledClient(2, "https://example.invalid:5986/wsman",
		WithRecorder(t.TempDir(), "probe"))
	if err == nil {
		t.Fatal("プール経由の録音を許してしまった (静かに空振る)")
	}
	if !strings.Contains(err.Error(), "NewPooledClient") {
		t.Errorf("エラーが理由を示していない: %v", err)
	}
}

// TestReplaceFold_NonASCII は大文字小文字を無視した置換が非 ASCII で壊れないことを検証する。
//
// ToLower はバイト長を変える文字がある (İ は小文字化で 2 → 3 バイト)。小文字化した
// 文字列のバイト位置を原文へ当てる実装だと、位置がずれてタグを壊し、伏せたい値の一部が残る。
func TestReplaceFold_NonASCII(t *testing.T) {
	for _, tt := range []struct {
		name, in, old, want string
	}{
		{"İ を含む前置き", "İstanbul <h>HV01</h>", "hv01", "İstanbul <h>X</h>"},
		{"ケルビン記号", "K <h>HV01</h>", "hv01", "K <h>X</h>"},
		{"大文字小文字混在", "<h>Hv01</h>", "hV01", "<h>X</h>"},
		{"該当なし", "<h>other</h>", "hv01", "<h>other</h>"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := replaceFold(tt.in, tt.old, "X"); got != tt.want {
				t.Errorf("replaceFold(%q, %q) = %q, want %q", tt.in, tt.old, got, tt.want)
			}
		})
	}
}

// TestPlaceholderFor_IPRange は伏せた IP が常に正しいアドレスになることを検証する。
//
// GUID と採番を共有していると、GUID を多く含む応答の後で 203.0.113.301 のような
// 不正な値を書いてしまう。録音器自身が実機に無い値を作ることになる。
func TestPlaceholderFor_IPRange(t *testing.T) {
	a := newAnonymizer("https://example.invalid/wsman", nil)
	// GUID を多めに消費してから IP を割り当てる。
	for i := 0; i < 300; i++ {
		a.placeholderFor("guid", fmt.Sprintf("guid-%d", i))
	}
	for i := 0; i < 600; i++ {
		got := a.placeholderFor("ip", fmt.Sprintf("src-%d", i))
		if net.ParseIP(got) == nil {
			t.Fatalf("%d 個目のプレースホルダが IP として不正: %q", i, got)
		}
	}
	// 決定的であること。
	first := a.placeholderFor("ip", "src-0")
	if second := a.placeholderFor("ip", "src-0"); first != second {
		t.Errorf("同じ入力に違う値を返した: %q → %q", first, second)
	}

	// **採番が他の種別に影響されないこと。** カウンタを共有していると、応答に含まれる
	// GUID の数で IP の番号が変わり、再録音の差分が読めなくなる。
	clean := newAnonymizer("https://example.invalid/wsman", nil)
	dirty := newAnonymizer("https://example.invalid/wsman", nil)
	for i := 0; i < 300; i++ {
		dirty.placeholderFor("guid", fmt.Sprintf("guid-%d", i))
	}
	if got, want := dirty.placeholderFor("ip", "src-x"), clean.placeholderFor("ip", "src-x"); got != want {
		t.Errorf("GUID の数で IP の採番が変わった: got %q, want %q", got, want)
	}
}

// TestWellFormedXML は匿名化後の整形式チェックが実際に壊れを捕まえることを確認する。
// 置換が XML を壊したまま fixture にすると、録音器自身が実機に無い形を作ることになる。
func TestWellFormedXML(t *testing.T) {
	if err := wellFormedXML("<a><b>x</b></a>"); err != nil {
		t.Errorf("整形式の XML を拒否した: %v", err)
	}
	if err := wellFormedXML("<a><b>x</a>"); err == nil {
		t.Error("閉じていないタグを見逃した")
	}
	if err := wellFormedXML("<a>x<"); err == nil {
		t.Error("途中で切れた XML を見逃した")
	}
}

// TestScrubPreservesClassNames は、伏せる名前が一般語でも CIM のクラス名を壊さないことを
// 検証する (#157)。
//
// スイッチ名は External / Internal が最も一般的で、これを素朴に全文置換すると
// Msvm_ExternalEthernetPort が Msvm_scrubbed-1EthernetPort になる。整形式のままなので
// 構造チェックでは捕まらず、**録音器が実在しないクラス名を持つ fixture を書く**。
func TestScrubPreservesClassNames(t *testing.T) {
	a := newAnonymizer("https://example.invalid/wsman", []string{"External", "hv01"})

	// ソースに XML を直接書かない (関所が禁じている迂回路なので)。
	in := strings.ReplaceAll(string(loadGolden(t, "synthetic/classname_probe.xml")),
		"__PRIVATE_PATH__", `C:\VMs\External\cfg.xml`)
	got := a.scrub(in)

	if !strings.Contains(got, "Msvm_ExternalEthernetPort") {
		t.Errorf("CIM クラス名が壊れた:\n%s", got)
	}
	// 要素テキストとしての出現は伏せる。
	if strings.Contains(got, ">External<") {
		t.Errorf("要素テキストの名前が伏せられていない:\n%s", got)
	}
	// パスの途中も伏せる (区切り文字は単語境界になる)。
	if strings.Contains(got, `VMs\External\`) {
		t.Errorf("パス中の名前が伏せられていない:\n%s", got)
	}
	if strings.Contains(got, "hv01") {
		t.Errorf("ホスト名が伏せられていない:\n%s", got)
	}
}

// TestScrubClassTokensUnchanged は、匿名化の前後で CIM クラス名の集合が変わらないことを
// 検査する仕組みが実際に働くことを確認する。
func TestScrubClassTokensUnchanged(t *testing.T) {
	origin := string(loadGolden(t, "synthetic/classname_probe.xml"))
	if err := classTokensUnchanged(origin, origin); err != nil {
		t.Errorf("変わっていないのにエラーにした: %v", err)
	}
	broken := strings.ReplaceAll(origin, "Msvm_ExternalEthernetPort", "Msvm_scrubbedEthernetPort")
	if err := classTokensUnchanged(origin, broken); err == nil {
		t.Error("クラス名の書き換えを見逃した")
	}
}

// TestScrubMACAddress は物理 NIC の MAC が伏せられることを検証する (#157)。
//
// 公開リポジトリに録音物を置く前提で、MAC は OUI からベンダが割れる。
// 12 桁 hex を無条件に置換すると GUID プレースホルダの末尾にも当たるので、要素単位で扱う。
func TestScrubMACAddress(t *testing.T) {
	// 実環境の MAC をテストに書かない (実際に一度やった)。RFC 7042 の文書用アドレスを使う。
	a := newAnonymizer("https://example.invalid/wsman", nil)
	got := a.scrub(`<p:PermanentAddress>00005E005301</p:PermanentAddress>` +
		`<p:InstanceID>Microsoft:5F1CA9D4-1111-2222-3333-444455556666</p:InstanceID>`)

	if strings.Contains(got, "00005E005301") {
		t.Errorf("MAC が伏せられていない: %s", got)
	}
	if !strings.Contains(got, "<p:PermanentAddress>00155D") {
		t.Errorf("MAC が Hyper-V の OUI に写っていない: %s", got)
	}
	// GUID のプレースホルダが MAC 置換で二重に壊れていないこと。
	if !regexp.MustCompile(`00000000-0000-4000-8000-[0-9a-f]{12}`).MatchString(got) {
		t.Errorf("GUID のプレースホルダが壊れた: %s", got)
	}
}

// TestScrubDifferencingDiskName は差分ディスク名の中の VM 名が伏せられることを検証する。
//
// Hyper-V の差分ディスクは <VM名>_<GUID>.avhdx という命名なので、"_" を単語文字として
// 扱うと VM 名が伏せられない。チェックポイントを 1 つでも持つ VM がいるホストでは
// 保存後検証が落ちて**録音自体が成立しなくなる**。
func TestScrubDifferencingDiskName(t *testing.T) {
	const vmName = "probe-vm-01"
	a := newAnonymizer("https://example.invalid/wsman", []string{vmName})
	got := a.scrub(`<p:Path>D:\VMs\probe-vm-01\probe-vm-01_5F1CA9D4-1111-2222-3333-444455556666.avhdx</p:Path>` +
		`<p:Class>Msvm_StorageAllocationSettingData</p:Class>`)

	if strings.Contains(got, vmName) {
		t.Errorf("差分ディスク名の中の VM 名が伏せられていない: %s", got)
	}
	// クラス名は壊さない。
	if !strings.Contains(got, "Msvm_StorageAllocationSettingData") {
		t.Errorf("CIM クラス名が壊れた: %s", got)
	}
}

// TestScrubMACAddress_WithAttributes は属性付きの要素でも MAC を伏せることを検証する。
// 属性の有無で網が抜けると、実機が形を変えた瞬間に漏れる。
func TestScrubMACAddress_WithAttributes(t *testing.T) {
	a := newAnonymizer("https://example.invalid/wsman", nil)
	got := a.scrub(`<p:PermanentAddress xsi:type="p:string">00005E005301</p:PermanentAddress>`)
	if strings.Contains(got, "00005E005301") {
		t.Errorf("属性付きの要素で MAC が伏せられていない: %s", got)
	}
	if err := verifyRecorded(recordedFile(
		`<p:PermanentAddress xsi:type="p:string">00005E005301</p:PermanentAddress>`), nil, ""); err == nil {
		t.Error("属性付きの要素で MAC の漏れを見逃した")
	}
}
