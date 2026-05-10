package wsman

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWithNTLM_ConfiguresTransport(t *testing.T) {
	t.Run("WithNTLM でクライアントが正常に作成される", func(t *testing.T) {
		client, err := NewClient("https://host:5986/wsman", WithNTLM("user", "pass"))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		if client == nil {
			t.Fatal("client is nil")
		}
	})

	t.Run("WithNTLMAuth でドメイン付きクライアントが作成される", func(t *testing.T) {
		client, err := NewClient("https://host:5986/wsman", WithNTLMAuth("DOMAIN", "user", "pass"))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		if client == nil {
			t.Fatal("client is nil")
		}
	})

	t.Run("WithNTLMAuth でドメインが空の場合はユーザー名のみ", func(t *testing.T) {
		client, err := NewClient("https://host:5986/wsman", WithNTLMAuth("", "user", "pass"))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		if client == nil {
			t.Fatal("client is nil")
		}
	})
}

func TestWithNTLM_NonAuthServer(t *testing.T) {
	t.Run("認証不要のサーバーに NTLM クライアントで接続できる", func(t *testing.T) {
		// Negotiator は 401 が来ない場合、リクエストをそのまま通す
		responseXML := loadGolden(t, "get_response_computersystem.xml")

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			_, _ = w.Write(responseXML)
		}))
		defer server.Close()

		// テストサーバーの自己署名証明書を許容するため WithInsecureSkipVerify を併用
		client, err := NewClient(server.URL, WithNTLM("testuser", "testpass"), WithInsecureSkipVerify())
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		resp, err := client.Get(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		name := resp.Property("Name")
		if name != "SERVER01" {
			t.Errorf("Name = %q, want %q", name, "SERVER01")
		}
	})
}

func TestHTTPTransport_WithCredentials(t *testing.T) {
	t.Run("credentials が設定された transport は BasicAuth を送信する", func(t *testing.T) {
		responseXML := loadGolden(t, "get_response_computersystem.xml")

		var gotAuth string
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			_, _ = w.Write(responseXML)
		}))
		defer server.Close()

		transport := NewHTTPTransport(server.URL, server.Client())
		transport.SetCredentials("testuser", "testpass")

		requestXML, _ := BuildGetRequest(
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem",
			server.URL,
		)

		_, err := transport.Send(context.Background(), requestXML)
		if err != nil {
			t.Fatalf("Send failed: %v", err)
		}

		if gotAuth == "" {
			t.Error("Authorization header was not set")
		}
		if !strings.HasPrefix(gotAuth, "Basic ") {
			t.Errorf("Authorization = %q, want prefix 'Basic '", gotAuth)
		}
	})

	t.Run("credentials なしの transport は Authorization を送信しない", func(t *testing.T) {
		responseXML := loadGolden(t, "get_response_computersystem.xml")

		var gotAuth string
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			_, _ = w.Write(responseXML)
		}))
		defer server.Close()

		transport := NewHTTPTransport(server.URL, server.Client())

		requestXML, _ := BuildGetRequest(
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem",
			server.URL,
		)

		_, err := transport.Send(context.Background(), requestXML)
		if err != nil {
			t.Fatalf("Send failed: %v", err)
		}

		if gotAuth != "" {
			t.Errorf("Authorization header should be empty, got %q", gotAuth)
		}
	})
}
