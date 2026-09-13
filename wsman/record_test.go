package wsman

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// guidPattern はカセットに実環境の GUID が残っていないか調べるための検査用パターン。
var guidPattern = regexp.MustCompile(`[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}`)

// TestWithRecorder は実機との通信を go-vcr のカセットに録音できること、
// および保存前に実環境の識別子が匿名化されることを検証する (#157)。
//
// golden を人が書く工程を無くすのが目的なので、「録音できる」だけでなく
// 「そのまま公開リポジトリに置ける状態で落ちる」ところまでを 1 単位とする。
func TestWithRecorder(t *testing.T) {
	body := loadGolden(t, "pull_response_xsinil_real.xml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	cassette := filepath.Join(t.TempDir(), "probe")
	client, err := NewClient(server.URL, WithRecorder(cassette))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	// 応答のパース結果はここでは問わない。録音は RoundTripper 層で行うため、
	// 上位がエラーを返しても記録される。
	_, _ = client.Enumerate(context.Background(), "http://example.invalid/Msvm_Test")

	if err := client.StopRecording(); err != nil {
		t.Fatalf("StopRecording: %v", err)
	}

	raw, err := os.ReadFile(cassette + ".yaml")
	if err != nil {
		t.Fatalf("カセットが保存されていない: %v", err)
	}
	got := string(raw)

	if !strings.Contains(got, "Msvm_ResourceAllocationSettingData") {
		t.Errorf("応答本文が記録されていない:\n%s", got)
	}

	// 匿名化: 元の応答に含まれる GUID がそのまま残っていないこと。
	for _, guid := range guidPattern.FindAllString(string(body), -1) {
		if strings.Contains(got, guid) {
			t.Errorf("GUID %q が匿名化されずにカセットへ残っている", guid)
		}
	}
	// ホスト名 (httptest のアドレス) も残さない。
	if host := strings.TrimPrefix(server.URL, "http://"); strings.Contains(got, host) {
		t.Errorf("接続先ホスト %q がカセットへ残っている", host)
	}

	// 決定的であること: 同じ GUID は同じプレースホルダに写る。
	// (毎回ランダムだと再録音時の差分が読めない)
	if n := strings.Count(got, "00000000-0000-0000-0000-000000000003"); n != 0 {
		t.Errorf("元の GUID が %d 箇所残っている", n)
	}
	placeholders := regexp.MustCompile(`00000000-0000-4000-8000-[0-9a-f]{12}`).FindAllString(got, -1)
	if len(placeholders) == 0 {
		t.Errorf("プレースホルダ GUID が 1 つも無い。匿名化が働いていない可能性:\n%s", got)
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

// TestWithRecorderScrub は、正規表現では拾えない任意の識別子 (VM 表示名・
// コンピュータ名など) を明示リストで伏せられること、そして **保存後に検証して
// 残っていたら落ちる**ことを検証する (#157)。
//
// 匿名化は「したつもり」が最も危ない。実機で録ったカセットをそのまま公開リポジトリへ
// 置く前提なので、漏れは静かに通さず fail-loud にする。
func TestWithRecorderScrub(t *testing.T) {
	const secretName = "k8s-cp-01"
	// プライベート IP をソースにリテラルで書かない。この検査自体がプライベート IP を
	// 必要とするが、リテラルで置くと CI の no-private-addresses に引っかかるうえ、
	// 実環境のアドレスを書き写す事故 (実際に一度やった) の入口になる。
	privateIP := net.IPv4(172, 20, 0, 5).String()
	body := []byte(`<?xml version="1.0"?><r><n>` + secretName + `</n><ip>` + privateIP + `</ip></r>`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	cassette := filepath.Join(t.TempDir(), "scrub")
	client, err := NewClient(server.URL, WithRecorder(cassette), WithRecorderScrub(secretName))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, _ = client.Enumerate(context.Background(), "http://example.invalid/Msvm_Test")
	if err := client.StopRecording(); err != nil {
		t.Fatalf("StopRecording: %v", err)
	}

	raw, err := os.ReadFile(cassette + ".yaml")
	if err != nil {
		t.Fatalf("カセットが保存されていない: %v", err)
	}
	got := string(raw)
	if strings.Contains(got, secretName) {
		t.Errorf("明示指定した %q がカセットに残っている", secretName)
	}
	// プライベート IP は指定しなくても伏せる (公開リポジトリの CI が落とす対象)。
	if strings.Contains(got, privateIP) {
		t.Errorf("プライベート IP がカセットに残っている")
	}
}

// TestRecorderVerifyCatchesLeak は保存後の検証が実際に漏れを捕まえることを確認する。
// 匿名化の実装を信じずに、出力そのものを検査していることの証明。
func TestRecorderVerifyCatchesLeak(t *testing.T) {
	leaked := net.IPv4(192, 168, 1, 10).String()
	err := verifyCassetteScrubbed([]byte("host: "+leaked+"\n"), nil, "")
	if err == nil {
		t.Fatal("プライベート IP を見逃した")
	}
	if !strings.Contains(err.Error(), leaked) {
		t.Errorf("エラーが漏れた値を示していない: %v", err)
	}

	if err := verifyCassetteScrubbed([]byte("host: hyperv-host.example.invalid\n"), nil, ""); err != nil {
		t.Errorf("匿名化済みの内容を誤って拒否した: %v", err)
	}
}

// TestNewReplayClient は録音したカセットをユニットテストで再生できることを検証する (#157)。
//
// これが無いと「手書き golden を禁止する」だけになって代替が無い。
// 録音 → 再生が閉じて初めて「golden を人が書く工程」を消せる。
func TestNewReplayClient(t *testing.T) {
	// まず httptest 相手に録音する (実機が無い CI でも回る形にするため)。
	// Enumerate は Enumerate → Pull の 2 往復なので、Action で応答を出し分ける。
	enumResp := loadGolden(t, "enumerate_response.xml")
	pullResp := loadGolden(t, "pull_response_xsinil_real.xml")
	endResp := loadGolden(t, "pull_response_end.xml")
	pulls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		if strings.Contains(string(reqBody), "enumeration/Enumerate") {
			_, _ = w.Write(enumResp)
			return
		}
		pulls++
		if pulls == 1 {
			_, _ = w.Write(pullResp)
			return
		}
		_, _ = w.Write(endResp) // EndOfSequence
	}))
	cassette := filepath.Join(t.TempDir(), "replay")
	rec, err := NewClient(server.URL, WithRecorder(cassette))
	if err != nil {
		t.Fatalf("NewClient(record): %v", err)
	}
	_, _ = rec.Enumerate(context.Background(), "http://example.invalid/Msvm_Test")
	if err := rec.StopRecording(); err != nil {
		t.Fatalf("StopRecording: %v", err)
	}
	server.Close() // 以降ネットワークは無い。再生できれば本当にカセット由来。

	replay, err := NewReplayClient(cassette, server.URL)
	if err != nil {
		t.Fatalf("NewReplayClient: %v", err)
	}
	items, err := replay.Enumerate(context.Background(), "http://example.invalid/Msvm_Test")
	if err != nil {
		t.Fatalf("再生に失敗: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("再生した応答からインスタンスが取れていない")
	}
	if got := items[0].PropertiesList()["ResourceType"]; len(got) != 1 || got[0] != "17" {
		t.Errorf("ResourceType = %v, want [17] (カセットの中身が再生されていない)", got)
	}
}
