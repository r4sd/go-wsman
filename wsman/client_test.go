package wsman

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestNewClient(t *testing.T) {
	t.Run("valid endpoint", func(t *testing.T) {
		_, err := NewClient("https://host:5986/wsman")
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
	})

	t.Run("invalid scheme", func(t *testing.T) {
		_, err := NewClient("ftp://host:5986/wsman")
		if err == nil {
			t.Fatal("expected error for ftp scheme")
		}
	})

	t.Run("missing host", func(t *testing.T) {
		_, err := NewClient("https://")
		if err == nil {
			t.Fatal("expected error for missing host")
		}
	})

	t.Run("empty string", func(t *testing.T) {
		_, err := NewClient("")
		if err == nil {
			t.Fatal("expected error for empty endpoint")
		}
	})
}

func TestClient_Get(t *testing.T) {
	t.Run("モックサーバーに対して Get が正しく動作", func(t *testing.T) {
		responseXML := loadGolden(t, "get_response_computersystem.xml")

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			_, _ = w.Write(responseXML)
		}))
		defer server.Close()

		client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		resp, err := client.Get(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem")
		if err != nil {
			t.Fatalf("Client.Get に失敗: %v", err)
		}

		name := resp.Property("Name")
		if name != "SERVER01" {
			t.Errorf("Name = %q, want %q", name, "SERVER01")
		}
	})

	t.Run("SelectorSet 付き Get", func(t *testing.T) {
		responseXML := loadGolden(t, "get_response_computersystem.xml")

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// リクエストに SelectorSet が含まれることを検証
			body, _ := io.ReadAll(r.Body)
			if len(body) == 0 {
				t.Error("リクエストボディが空")
			}
			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			_, _ = w.Write(responseXML)
		}))
		defer server.Close()

		client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		resp, err := client.Get(context.Background(),
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service",
			Selector{Name: "Name", Value: "WinRM"},
		)
		if err != nil {
			t.Fatalf("Client.Get に失敗: %v", err)
		}

		if resp == nil {
			t.Fatal("レスポンスが nil")
		}
	})

	t.Run("Fault レスポンスでエラーを返す", func(t *testing.T) {
		faultXML := loadGolden(t, "fault_access_denied.xml")

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write(faultXML)
		}))
		defer server.Close()

		client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, err = client.Get(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_OperatingSystem")
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

func TestClient_Enumerate(t *testing.T) {
	t.Run("Enumerate → Pull → EndOfSequence の全フロー", func(t *testing.T) {
		enumResponseXML := loadGolden(t, "enumerate_response.xml")
		pullResponseXML := loadGolden(t, "pull_response.xml")
		pullEndResponseXML := loadGolden(t, "pull_response_end.xml")

		var requestCount atomic.Int32

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			count := requestCount.Add(1)
			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")

			switch count {
			case 1:
				_, _ = w.Write(enumResponseXML)
			case 2:
				_, _ = w.Write(pullResponseXML)
			case 3:
				_, _ = w.Write(pullEndResponseXML)
			default:
				t.Errorf("予期しないリクエスト (count=%d)", count)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		defer server.Close()

		client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		instances, err := client.Enumerate(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process")
		if err != nil {
			t.Fatalf("Client.Enumerate に失敗: %v", err)
		}

		// 2件 + 1件 = 3件
		if len(instances) != 3 {
			t.Fatalf("インスタンス数 = %d, want 3", len(instances))
		}

		// 各インスタンスのプロパティを確認
		if instances[0].Property("Name") != "System Idle Process" {
			t.Errorf("instances[0].Name = %q, want %q", instances[0].Property("Name"), "System Idle Process")
		}
		if instances[1].Property("Name") != "System" {
			t.Errorf("instances[1].Name = %q, want %q", instances[1].Property("Name"), "System")
		}
		if instances[2].Property("Name") != "svchost.exe" {
			t.Errorf("instances[2].Name = %q, want %q", instances[2].Property("Name"), "svchost.exe")
		}

		// リクエスト数の確認
		if requestCount.Load() != 3 {
			t.Errorf("リクエスト数 = %d, want 3 (Enumerate + 2 Pull)", requestCount.Load())
		}
	})

	t.Run("Enumerate 時の Fault でエラーを返す", func(t *testing.T) {
		faultXML := loadGolden(t, "fault_access_denied.xml")

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write(faultXML)
		}))
		defer server.Close()

		client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, err = client.Enumerate(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process")
		if err == nil {
			t.Fatal("Fault レスポンスでエラーが返されなかった")
		}
	})
}
