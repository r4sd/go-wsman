package wsman

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildCreateRequest(t *testing.T) {
	t.Run("基本の Create リクエスト生成", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process"
		properties := map[string]string{
			"CommandLine": "notepad.exe",
		}

		data, err := BuildCreateRequest(resourceURI, "http://host:5986/wsman", properties)
		if err != nil {
			t.Fatalf("BuildCreateRequest に失敗: %v", err)
		}

		// パースして検証
		env, err := UnmarshalEnvelope(data)
		if err != nil {
			t.Fatalf("生成された XML のパースに失敗: %v", err)
		}

		// Action が ActionCreate であることを検証
		if env.Header.Action == nil || env.Header.Action.Value != ActionCreate {
			t.Errorf("Action = %v, want %q", env.Header.Action, ActionCreate)
		}

		// ResourceURI
		if env.Header.ResourceURI == nil || env.Header.ResourceURI.Value != resourceURI {
			t.Errorf("ResourceURI = %v, want %q", env.Header.ResourceURI, resourceURI)
		}

		// To
		if env.Header.To == nil || env.Header.To.Value != "http://host:5986/wsman" {
			t.Errorf("To = %v, want %q", env.Header.To, "http://host:5986/wsman")
		}

		// ReplyTo
		if env.Header.ReplyTo == nil || env.Header.ReplyTo.Address.Value != AddressAnonymous {
			t.Error("ReplyTo が正しく設定されていない")
		}

		// MessageID
		if env.Header.MessageID == nil || env.Header.MessageID.Value == "" {
			t.Error("MessageID が設定されていない")
		}

		// SelectorSet は不要（新規作成なので nil であること）
		if env.Header.SelectorSet != nil {
			t.Error("Create リクエストに SelectorSet が含まれている（不要）")
		}

		// Body にプロパティ XML が含まれることを検証
		bodyStr := string(env.Body.Content)
		if !strings.Contains(bodyStr, "Win32_Process") {
			t.Error("Body にクラス名 Win32_Process が含まれていない")
		}
		if !strings.Contains(bodyStr, "CommandLine") {
			t.Error("Body にプロパティ CommandLine が含まれていない")
		}
		if !strings.Contains(bodyStr, "notepad.exe") {
			t.Error("Body にプロパティ値 notepad.exe が含まれていない")
		}
		if !strings.Contains(bodyStr, resourceURI) {
			t.Error("Body に名前空間 URI が含まれていない")
		}
	})

	t.Run("空のプロパティでエラー", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process"
		properties := map[string]string{}

		_, err := BuildCreateRequest(resourceURI, "http://host:5986/wsman", properties)
		if err == nil {
			t.Fatal("空のプロパティでエラーが返されなかった")
		}
	})
}

func TestParseCreateResponse(t *testing.T) {
	t.Run("CreateResponse から EndpointReference を抽出", func(t *testing.T) {
		data := loadGolden(t, "create_response_process.xml")

		resp, err := ParseCreateResponse(data)
		if err != nil {
			t.Fatalf("ParseCreateResponse に失敗: %v", err)
		}

		// ResourceURI を検証
		wantURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process"
		if resp.ResourceURI != wantURI {
			t.Errorf("ResourceURI = %q, want %q", resp.ResourceURI, wantURI)
		}

		// Selectors を検証
		if len(resp.Selectors) != 1 {
			t.Fatalf("Selectors 数 = %d, want 1", len(resp.Selectors))
		}
		handle, ok := resp.Selectors["Handle"]
		if !ok {
			t.Fatal("Selector 'Handle' が存在しない")
		}
		if handle != "12345" {
			t.Errorf("Selector Handle = %q, want %q", handle, "12345")
		}
	})

	t.Run("Fault レスポンスはエラーを返す", func(t *testing.T) {
		data := loadGolden(t, "fault_access_denied.xml")

		_, err := ParseCreateResponse(data)
		if err == nil {
			t.Fatal("Fault レスポンスでエラーが返されなかった")
		}

		fault, ok := err.(*Fault)
		if !ok {
			t.Fatalf("エラーが *Fault 型ではない: %T", err)
		}
		if fault.Subcode != "w:AccessDenied" {
			t.Errorf("Fault.Subcode = %q, want %q", fault.Subcode, "w:AccessDenied")
		}
	})
}

func TestClient_Create(t *testing.T) {
	t.Run("モックサーバーに対して Create が正しく動作", func(t *testing.T) {
		responseXML := loadGolden(t, "create_response_process.xml")

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// リクエストに Create 操作のボディが含まれることを検証
			body, _ := io.ReadAll(r.Body)
			if len(body) == 0 {
				t.Error("リクエストボディが空")
			}

			// リクエストに ActionCreate が含まれることを検証
			if !strings.Contains(string(body), ActionCreate) {
				t.Error("リクエストに ActionCreate が含まれていない")
			}

			// リクエストにプロパティが含まれることを検証
			if !strings.Contains(string(body), "CommandLine") {
				t.Error("リクエストにプロパティ CommandLine が含まれていない")
			}

			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			_, _ = w.Write(responseXML)
		}))
		defer server.Close()

		client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		properties := map[string]string{
			"CommandLine": "notepad.exe",
		}

		resp, err := client.Create(context.Background(),
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process",
			properties,
		)
		if err != nil {
			t.Fatalf("Client.Create に失敗: %v", err)
		}

		// 作成されたインスタンスの情報を検証
		wantURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process"
		if resp.ResourceURI != wantURI {
			t.Errorf("ResourceURI = %q, want %q", resp.ResourceURI, wantURI)
		}

		handle, ok := resp.Selectors["Handle"]
		if !ok {
			t.Fatal("Selector 'Handle' が存在しない")
		}
		if handle != "12345" {
			t.Errorf("Selector Handle = %q, want %q", handle, "12345")
		}
	})
}
