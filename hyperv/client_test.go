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

// TestClient_ListComputerSystems は Enumerate で全 VM を取得するテスト。
func TestClient_ListComputerSystems(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_computersystem.xml")
	pullXML := loadGolden(t, "pull_response_computersystem.xml")

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

	got, err := client.ListComputerSystems(context.Background())
	if err != nil {
		t.Fatalf("ListComputerSystems: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len: got %d, want 2", len(got))
	}
	if got[0].ElementName != "vm-1" {
		t.Errorf("got[0].ElementName: got %q", got[0].ElementName)
	}
	if got[1].ElementName != "vm-2" {
		t.Errorf("got[1].ElementName: got %q", got[1].ElementName)
	}
	if got[0].EnabledState != EnabledStateEnabled {
		t.Errorf("got[0].EnabledState: got %d", got[0].EnabledState)
	}
	if got[1].EnabledState != EnabledStateDisabled {
		t.Errorf("got[1].EnabledState: got %d", got[1].EnabledState)
	}
}

// TestClient_FindComputerSystemByElementName は表示名 (ElementName) から VM を引く。
//
// provider 側の VM CRUD は表示名で操作するが、CIM の各操作 (GetSystemSettingData /
// DestroySystem / RequestStateChange 等) は VM GUID を要求する。本メソッドは
// 表示名→GUID 解決の入口となる。サーバー側 WQL で絞った上で ElementName 完全一致を
// クライアント側でも確認する (WQL の部分一致・大小文字のクセに依存しない)。
func TestClient_FindComputerSystemByElementName(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_computersystem.xml")
	pullXML := loadGolden(t, "pull_response_computersystem.xml")

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

	// golden は vm-1 / vm-2 を返す。表示名 "vm-2" を正しく選択できること。
	got, err := client.FindComputerSystemByElementName(context.Background(), "vm-2")
	if err != nil {
		t.Fatalf("FindComputerSystemByElementName: %v", err)
	}
	if got.ElementName != "vm-2" {
		t.Errorf("ElementName: got %q, want vm-2", got.ElementName)
	}
	// 表示名が WQL フィルタとしてサーバーに送られていること。
	// SOAP では引用符が XML エスケープされる (" → &#34;)。
	if !strings.Contains(enumBody, `ElementName=&#34;vm-2&#34;`) {
		t.Errorf("WQL ElementName filter not sent; enumerate body: %s", enumBody)
	}
}

// TestClient_FindComputerSystemByElementName_NotFound は該当 VM が無い場合にエラーを返す。
//
// テストサーバーは WQL を解さず golden (vm-1 / vm-2) をそのまま返すため、クライアント側
// の ElementName 完全一致フィルタが「不在」を正しく検出することを検証する。
func TestClient_FindComputerSystemByElementName_NotFound(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_computersystem.xml")
	pullXML := loadGolden(t, "pull_response_computersystem.xml")

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
	_, err = client.FindComputerSystemByElementName(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing VM, got nil")
	}
	// 不在は sentinel error にラップされ、provider 側で errors.Is 判定できること。
	if !errors.Is(err, ErrVMNotFound) {
		t.Errorf("expected errors.Is(err, ErrVMNotFound), got %v", err)
	}
}

// TestClient_FindComputerSystemByElementName_EscapesSpecialChars は表示名に含まれる
// 特殊文字 (& / ") が安全にエスケープされて送信されることを検証する。
//
//   - & は XML 自体を壊す (エスケープ漏れ = 通信失敗)
//   - " は WQL リテラルを破壊しうる (フィルタ改竄)
//
// 送信された Enumerate ボディが整形式 XML であり、& が XML エスケープ、" が
// WQL バックスラッシュ + XML エスケープ (\&#34;) されていることを確認する。
func TestClient_FindComputerSystemByElementName_EscapesSpecialChars(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_computersystem.xml")
	pullXML := loadGolden(t, "pull_response_computersystem.xml")

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

	// golden に一致 VM は無いので戻り値はエラーだが、検証対象は送信ボディ。
	_, _ = client.FindComputerSystemByElementName(context.Background(), `a&b"c`)

	// 1) SOAP ボディが整形式 XML であること (エスケープ漏れなら & で壊れる)。
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
	// 2) & が XML エスケープされている。
	if !strings.Contains(enumBody, "a&amp;b") {
		t.Errorf("& not XML-escaped; body=%s", enumBody)
	}
	// 3) WQL リテラルの " がバックスラッシュ + XML エスケープされている (\&#34;)。
	if !strings.Contains(enumBody, `\&#34;`) {
		t.Errorf(`" not WQL+XML-escaped (expected \&#34;); body=%s`, enumBody)
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
