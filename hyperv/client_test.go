package hyperv

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/r4sd/go-wsman/wsman"
)

func loadGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("failed to load golden file %s: %v", name, err)
	}
	return string(data)
}

// TestClient_GetComputerSystem は Get で単一 VM を取得するテスト。
func TestClient_GetComputerSystem(t *testing.T) {
	respXML := loadGolden(t, "get_response_computersystem.xml")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(respXML))
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := client.GetComputerSystem(context.Background(), "5C5E2D70-1111-2222-3333-444455556666")
	if err != nil {
		t.Fatalf("GetComputerSystem: %v", err)
	}

	if got.Name != "5C5E2D70-1111-2222-3333-444455556666" {
		t.Errorf("Name: got %q", got.Name)
	}
	if got.ElementName != "test-vm" {
		t.Errorf("ElementName: got %q", got.ElementName)
	}
	if got.EnabledState != EnabledStateEnabled {
		t.Errorf("EnabledState: got %d, want %d (Enabled)", got.EnabledState, EnabledStateEnabled)
	}
	if got.HealthState != 5 {
		t.Errorf("HealthState: got %d, want 5", got.HealthState)
	}
}

// TestNewPooledClient は wsman.NewPooledClient をラップした hyperv.NewPooledClient が
// 通常の Client と同じ API で機能することを検証する (#117 の並行 NTLM 401 対策)。
func TestNewPooledClient(t *testing.T) {
	respXML := loadGolden(t, "get_response_computersystem.xml")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(respXML))
	}))
	defer server.Close()

	client, err := NewPooledClient(3, server.URL)
	if err != nil {
		t.Fatalf("NewPooledClient: %v", err)
	}

	got, err := client.GetComputerSystem(context.Background(), "5C5E2D70-1111-2222-3333-444455556666")
	if err != nil {
		t.Fatalf("GetComputerSystem: %v", err)
	}
	if got.Name != "5C5E2D70-1111-2222-3333-444455556666" {
		t.Errorf("Name: got %q", got.Name)
	}
}

// TestClient_ListComputerSystems は Enumerate で全 VM を取得するテスト。
//
// 実機記録を使う (#186)。手書き golden は「1 Pull に VM 2 件」という
// **このクライアントが実機から受け取ることのない形**だった
// (MaxElements を送らないので WS-Enumeration の既定で 1 件 / Pull)。
//
// このテストの担当は **EnabledState を正しく読むこと**。
// ホスト除外の配線は host_exclusion_wiring_test.go が見る。
func TestClient_ListComputerSystems(t *testing.T) {
	var bodies []string
	server := newSequenceServer(t, recordedComputerSystemSequence(t), &bodies)
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := client.ListComputerSystems(context.Background())
	if err != nil {
		t.Fatalf("ListComputerSystems: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4 (記録はホスト 1 + VM 4)", len(got))
	}

	// 🔴 **EnabledState は VM ごとに違う値を期待する。** 記録に 2 と 3 の両方が
	// 入っているので、「全部同じ値を返す」実装では落ちる。
	// 順序には依存させない (API は順序を約束していない)。
	wantStates := map[string]uint16{
		recordedVMNameRunning: EnabledStateEnabled,  // 2
		"scrubbed-8":          EnabledStateEnabled,  // 2
		recordedVMNameOff:     EnabledStateDisabled, // 3
		"scrubbed-4":          EnabledStateEnabled,  // 2
	}
	gotStates := make(map[string]uint16, len(got))
	for _, cs := range got {
		gotStates[cs.ElementName] = cs.EnabledState
	}
	for name, want := range wantStates {
		state, ok := gotStates[name]
		if !ok {
			t.Errorf("VM %q が結果に無い: %v", name, gotStates)
			continue
		}
		if state != want {
			t.Errorf("%q の EnabledState = %d, want %d", name, state, want)
		}
	}
	if len(gotStates) != len(wantStates) {
		t.Errorf("VM が %d 件 (期待 %d 件): %v", len(gotStates), len(wantStates), gotStates)
	}
}

// TestClient_FindComputerSystemByElementName は表示名 (ElementName) から VM を引く。
//
// provider 側の VM CRUD は表示名で操作するが、CIM の各操作 (GetSystemSettingData /
// DestroySystem / RequestStateChange 等) は VM GUID を要求する。本メソッドは
// 表示名→GUID 解決の入口となる。Hyper-V は WQL フィルタ列挙を拒否する (#80) ため、
// 無フィルタ列挙 + クライアント側の ElementName 完全一致で絞り込む。
func TestClient_FindComputerSystemByElementName(t *testing.T) {
	var bodies []string
	server := newSequenceServer(t, recordedComputerSystemSequence(t), &bodies)
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// 記録の中の 1 台を表示名で選べること。**先頭でも末尾でもない**ものを選ぶ
	// (「先頭を返す」「最後を返す」だけの実装を落とすため)。
	got, err := client.FindComputerSystemByElementName(context.Background(), recordedVMNameOff)
	if err != nil {
		t.Fatalf("FindComputerSystemByElementName: %v", err)
	}
	if got.ElementName != recordedVMNameOff {
		t.Errorf("ElementName: got %q, want %q", got.ElementName, recordedVMNameOff)
	}
	// Hyper-V は WQL フィルタ列挙を拒否するため、Enumerate は無フィルタで送られること
	// (WQL Filter を含めると実機で CannotProcessFilter Fault になる。#80)。
	if len(bodies) == 0 {
		t.Fatal("リクエストが 1 本も飛んでいない")
	}
	if strings.Contains(bodies[0], "Filter") || strings.Contains(bodies[0], "SELECT") {
		t.Errorf("enumerate should be unfiltered (no WQL Filter); body: %s", bodies[0])
	}
}

// TestClient_FindComputerSystemByElementName_NotFound は該当 VM が無い場合にエラーを返す。
//
// テストサーバーは WQL を解さず記録をそのまま返すため、クライアント側の
// ElementName 完全一致フィルタが「不在」を正しく検出することを検証する。
func TestClient_FindComputerSystemByElementName_NotFound(t *testing.T) {
	var bodies []string
	server := newSequenceServer(t, recordedComputerSystemSequence(t), &bodies)
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.FindComputerSystemByElementName(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing VM, got nil")
	}
	// 不在は sentinel error にラップされ、provider 側で errors.Is 判定できること。
	if !errors.Is(err, ErrVMNotFound) {
		t.Errorf("expected errors.Is(err, ErrVMNotFound), got %v", err)
	}
}

// TestClient_FindComputerSystemByElementName_CaseInsensitive は表示名の照合が大小文字を
// 区別しないこと (PowerShell Get-VM との parity) を検証する。
//
// Hyper-V / PowerShell の VM 名照合は大小文字非依存。ここを case-sensitive にすると、実在する
// VM を大文字で引いたときに ErrVMNotFound となり、terraform-provider の Read が
// 「実在 VM を不在扱い→state 除去→orphan/重複作成」する破壊経路を生む。よって最終照合は
// case-insensitive とし、大小文字だけ異なる表示名でも同一 VM を解決する。
func TestClient_FindComputerSystemByElementName_CaseInsensitive(t *testing.T) {
	var bodies []string
	server := newSequenceServer(t, recordedComputerSystemSequence(t), &bodies)
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	upper := strings.ToUpper(recordedVMNameOff)
	if upper == recordedVMNameOff {
		t.Fatalf("検査名 %q に大文字化される文字が無い。大小文字非依存を確かめられない", recordedVMNameOff)
	}
	got, err := client.FindComputerSystemByElementName(context.Background(), upper)
	if err != nil {
		t.Fatalf("case-insensitive 照合で %q は %q に解決される想定、got err %v",
			upper, recordedVMNameOff, err)
	}
	if got.ElementName != recordedVMNameOff {
		t.Errorf("ElementName: got %q, want %q", got.ElementName, recordedVMNameOff)
	}
}

// TestClient_FindComputerSystemByElementName_SpecialChars は表示名に特殊文字 (& / ")
// が含まれていても安全に扱えることを検証する。
//
// 無フィルタ列挙 + クライアント側マッチに切り替えた (#80) ため、ElementName は WQL
// リテラルとして送信されず、エスケープの心配はない。検証ポイント:
//   - 送信される Enumerate は整形式 XML かつ WQL フィルタを含まない (実機 Fault 回避)
//   - 特殊文字を含む表示名でもクライアント側マッチで正しく VM を引ける
func TestClient_FindComputerSystemByElementName_SpecialChars(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_computersystem.xml")
	// & を含む表示名の VM を 1 件返す pull レスポンス (XML 上は &amp; でエスケープ)。
	pullXML := `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:e="http://schemas.xmlsoap.org/ws/2004/09/enumeration" xmlns:p="http://schemas.microsoft.com/wbem/wsman/1/wmi/root/virtualization/v2/Msvm_ComputerSystem">
  <s:Body>
    <e:PullResponse>
      <e:Items>
        <p:Msvm_ComputerSystem>
          <p:Name>aaaaaaaa-0000-1111-2222-333344445555</p:Name>
          <p:ElementName>a&amp;b"c</p:ElementName>
          <p:EnabledState>2</p:EnabledState>
          <p:HealthState>5</p:HealthState>
        </p:Msvm_ComputerSystem>
      </e:Items>
      <e:EndOfSequence/>
    </e:PullResponse>
  </s:Body>
</s:Envelope>`

	var enumBody string
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		callCount++
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		if callCount == 1 {
			enumBody = string(body)
			_, _ = w.Write([]byte(enumXML))
		} else {
			_, _ = w.Write([]byte(pullXML))
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := client.FindComputerSystemByElementName(context.Background(), `a&b"c`)
	if err != nil {
		t.Fatalf("FindComputerSystemByElementName: %v", err)
	}
	if got.ElementName != `a&b"c` {
		t.Errorf("ElementName: got %q, want %q", got.ElementName, `a&b"c`)
	}

	// 1) SOAP ボディが整形式 XML であること。
	dec := xml.NewDecoder(strings.NewReader(enumBody))
	for {
		_, derr := dec.Token()
		if derr == io.EOF {
			break
		}
		if derr != nil {
			t.Fatalf("enumerate body is not well-formed XML: %v\nbody=%s", derr, enumBody)
		}
	}
	// 2) WQL フィルタが含まれないこと (含めると実機で CannotProcessFilter Fault)。
	if strings.Contains(enumBody, "Filter") || strings.Contains(enumBody, "SELECT") {
		t.Errorf("enumerate should be unfiltered (no WQL Filter); body=%s", enumBody)
	}
}

// TestClient_FindComputerSystemByElementName_MultipleMatch は同名 VM が複数存在する
// 場合にエラーを返すことを検証する (黙って最初の1件を返すと誤 VM を破壊しうる)。
func TestClient_FindComputerSystemByElementName_MultipleMatch(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_computersystem.xml")
	pullXML := loadGolden(t, "pull_response_computersystem_dup.xml")

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		if callCount == 1 {
			_, _ = w.Write([]byte(enumXML))
		} else {
			_, _ = w.Write([]byte(pullXML))
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.FindComputerSystemByElementName(context.Background(), "dup-vm"); err == nil {
		t.Fatal("expected error for multiple matching VMs, got nil")
	}
}

// TestClient_FindComputerSystemByElementName_EmptyName は空名でエラーを返す (通信前に弾く)。
func TestClient_FindComputerSystemByElementName_EmptyName(t *testing.T) {
	client, err := NewClient("http://example.invalid")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.FindComputerSystemByElementName(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty elementName, got nil")
	}
}

// TestClient_NewClient は wsman.ClientOption が正しく伝播することを検証する。
func TestClient_NewClient_WithOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, wsman.WithTimeout(0))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client == nil {
		t.Fatal("client should not be nil")
	}
}

// TestClient_GetComputerSystem_SelectorSet は Get のリクエストに正しい鍵が
// 載っていることを検証する。
//
// Name セレクタだけでは実機が DestinationUnreachable を返す (2026-09-14 実機確認)。
// SelectorSet 全体を厳密比較する。Contains だと余計な Selector が増えても通る。
func TestClient_GetComputerSystem_SelectorSet(t *testing.T) {
	respXML := loadGolden(t, "get_response_computersystem.xml")

	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		body = string(b)
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(respXML))
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.GetComputerSystem(context.Background(), "5C5E2D70-1111-2222-3333-444455556666"); err != nil {
		t.Fatalf("GetComputerSystem: %v", err)
	}

	const want = `<w:Selector Name="CreationClassName">Msvm_ComputerSystem</w:Selector>` +
		`<w:Selector Name="Name">5C5E2D70-1111-2222-3333-444455556666</w:Selector>`
	// Get のエンベロープは整形されて改行・インデントが入るため、要素間の空白を畳む。
	got := collapseTagWhitespace(selectorSetInner(t, unescapeForAssert(body)))
	if got != want {
		t.Errorf("SelectorSet が一致しない\n got:  %s\n want: %s", got, want)
	}
}

// collapseTagWhitespace は要素と要素の間にある空白 (改行・インデント) を取り除く。
// 要素の中身には触らないので、Selector の値に含まれる空白は潰さない。
func collapseTagWhitespace(s string) string {
	return strings.TrimSpace(betweenTagsWhitespace.ReplaceAllString(s, "><"))
}

var betweenTagsWhitespace = regexp.MustCompile(`>\s+<`)
