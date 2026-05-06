package hyperv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newStateChangeServer は RequestStateChange の golden file を返す httptest server を作る。
// 受信したリクエストボディは *capturedBody に格納される。
func newStateChangeServer(t *testing.T, capturedBody *string) *httptest.Server {
	t.Helper()
	respXML := loadGolden(t, "invoke_response_request_state_change.xml")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		*capturedBody = string(body)
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(respXML))
	}))
}

// TestClient_RequestStateChange は汎用の RequestStateChange を呼び出すテスト。
//
// リクエストボディに RequestedState 値と対象 VM の Selector が
// 正しく含まれること、レスポンスから Job 参照が抽出できることを検証する。
func TestClient_RequestStateChange(t *testing.T) {
	var body string
	server := newStateChangeServer(t, &body)
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	jobRef, err := client.RequestStateChange(context.Background(),
		"11111111-aaaa-bbbb-cccc-000000000001", RequestedStateEnabled)
	if err != nil {
		t.Fatalf("RequestStateChange: %v", err)
	}

	// extractProperties が EPR 内の最後の非空 CharData (= Selector InstanceID) を抽出する
	if jobRef != "9C7D3E22-AAAA-BBBB-CCCC-111122223333" {
		t.Errorf("jobRef: got %q", jobRef)
	}

	// リクエスト検証
	checks := []string{
		"RequestStateChange",
		"<p:RequestedState>2</p:RequestedState>", // Enabled = 2
		"11111111-aaaa-bbbb-cccc-000000000001",   // 対象 VM の GUID (Selector)
		`Name="Name"`,                            // SelectorSet のキー名
	}
	for _, want := range checks {
		if !strings.Contains(body, want) {
			t.Errorf("request body missing %q\nbody: %s", want, body)
		}
	}
}

// TestClient_RequestStateChange_EmptyName は vmName 空のとき即エラー。
func TestClient_RequestStateChange_EmptyName(t *testing.T) {
	client, err := NewClient("http://localhost")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.RequestStateChange(context.Background(), "", RequestedStateEnabled); err == nil {
		t.Error("expected error for empty vmName")
	}
}

// TestClient_StateShortcuts はショートカットメソッドが正しい RequestedState を送ることを検証する。
//
// 各ショートカットがどの値を送るかは仕様の核心なので、テーブル駆動でカバーする。
func TestClient_StateShortcuts(t *testing.T) {
	tests := []struct {
		name     string
		invoke   func(c *Client, ctx context.Context, vmName string) (string, error)
		wantElem string // リクエストボディに含まれるべき RequestedState 要素
	}{
		{"StartVM", (*Client).StartVM, "<p:RequestedState>2</p:RequestedState>"},
		{"TurnOffVM", (*Client).TurnOffVM, "<p:RequestedState>3</p:RequestedState>"},
		{"ShutdownVM", (*Client).ShutdownVM, "<p:RequestedState>4</p:RequestedState>"},
		{"PauseVM", (*Client).PauseVM, "<p:RequestedState>32768</p:RequestedState>"},
		{"ResumeVM", (*Client).ResumeVM, "<p:RequestedState>2</p:RequestedState>"},
		{"SaveVM", (*Client).SaveVM, "<p:RequestedState>32769</p:RequestedState>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body string
			server := newStateChangeServer(t, &body)
			defer server.Close()

			client, err := NewClient(server.URL)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}

			if _, err := tt.invoke(client, context.Background(), "vm-guid"); err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			if !strings.Contains(body, tt.wantElem) {
				t.Errorf("%s: body missing %q", tt.name, tt.wantElem)
			}
		})
	}
}
