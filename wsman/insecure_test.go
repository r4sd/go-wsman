package wsman

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Azure/go-ntlmssp"
)

// TestWithInsecureSkipVerify_Default はデフォルト動作 (証明書検証あり) を検証する。
//
// 自己署名証明書のテストサーバーに WithInsecureSkipVerify なしで接続すると失敗する。
// これは CodeQL の go/disabled-certificate-check に応える挙動の中核。
func TestWithInsecureSkipVerify_Default(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, WithNTLM("u", "p"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.Get(context.Background(), "http://schemas.microsoft.com/foo")
	if err == nil {
		t.Fatal("expected TLS verify error without WithInsecureSkipVerify")
	}
}

// TestWithInsecureSkipVerify_Enabled は WithInsecureSkipVerify を渡したときに
// 自己署名証明書のサーバーに接続できることを検証する。
func TestWithInsecureSkipVerify_Enabled(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body/></s:Envelope>`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, WithNTLM("u", "p"), WithInsecureSkipVerify())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if _, err := client.Get(context.Background(), "http://schemas.microsoft.com/foo"); err != nil {
		t.Fatalf("Get with InsecureSkipVerify: %v", err)
	}
}

// TestWithInsecureSkipVerify_OrderIndependent は WithInsecureSkipVerify と
// WithNTLM の指定順序が結果に影響しないことを検証する (NewClient 終端で適用するため)。
func TestWithInsecureSkipVerify_OrderIndependent(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body/></s:Envelope>`))
	}))
	defer server.Close()

	tests := []struct {
		name string
		opts []ClientOption
	}{
		{
			name: "Insecure → NTLM",
			opts: []ClientOption{WithInsecureSkipVerify(), WithNTLM("u", "p")},
		},
		{
			name: "NTLM → Insecure",
			opts: []ClientOption{WithNTLM("u", "p"), WithInsecureSkipVerify()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(server.URL, tt.opts...)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if _, err := client.Get(context.Background(), "http://schemas.microsoft.com/foo"); err != nil {
				t.Errorf("Get: %v", err)
			}
		})
	}
}

// TestApplyInsecureSkipVerify_TLSConfigStruct は applyInsecureSkipVerify が
// transport の TLSClientConfig を正しく更新することを直接検証する。
func TestApplyInsecureSkipVerify_TLSConfigStruct(t *testing.T) {
	t.Run("plain http.Transport", func(t *testing.T) {
		t.Parallel()
		httpTransport := &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		}
		ht := &HTTPTransport{
			httpClient: &http.Client{Transport: httpTransport},
		}
		applyInsecureSkipVerify(ht)
		if !httpTransport.TLSClientConfig.InsecureSkipVerify {
			t.Error("InsecureSkipVerify should be true after applyInsecureSkipVerify")
		}
	})

	t.Run("ntlmssp wrapped transport", func(t *testing.T) {
		t.Parallel()
		inner := &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		}
		ht := &HTTPTransport{
			httpClient: &http.Client{
				Transport: &ntlmssp.Negotiator{RoundTripper: inner},
			},
		}
		applyInsecureSkipVerify(ht)
		if !inner.TLSClientConfig.InsecureSkipVerify {
			t.Error("InsecureSkipVerify should be true on inner transport (ntlm wrapped)")
		}
	})
}
