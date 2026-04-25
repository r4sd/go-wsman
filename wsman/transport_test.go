package wsman

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestOptimizedTransport(t *testing.T) {
	t.Run("プール設定が WS-Man に最適化されている", func(t *testing.T) {
		tr := optimizedTransport(nil)

		if tr.MaxIdleConns != 100 {
			t.Errorf("MaxIdleConns = %d, want 100", tr.MaxIdleConns)
		}
		if tr.MaxIdleConnsPerHost != 10 {
			t.Errorf("MaxIdleConnsPerHost = %d, want 10", tr.MaxIdleConnsPerHost)
		}
		if tr.IdleConnTimeout != 90*time.Second {
			t.Errorf("IdleConnTimeout = %v, want 90s", tr.IdleConnTimeout)
		}
	})

	t.Run("TLS 設定が引き継がれる", func(t *testing.T) {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true, //#nosec G402
		}
		tr := optimizedTransport(tlsConfig)

		if tr.TLSClientConfig != tlsConfig {
			t.Error("TLSClientConfig が設定されていない")
		}
	})

	t.Run("TLS nil でも動作する", func(t *testing.T) {
		tr := optimizedTransport(nil)

		if tr.TLSClientConfig != nil {
			t.Error("TLSClientConfig が nil であるべき")
		}
	})
}

func TestWithTimeout(t *testing.T) {
	t.Run("デフォルトタイムアウトは 60 秒", func(t *testing.T) {
		client, err := NewClient("https://host:5986/wsman")
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		timeout := client.transport.httpClient.Timeout
		if timeout != 60*time.Second {
			t.Errorf("Timeout = %v, want 60s", timeout)
		}
	})

	t.Run("WithTimeout でタイムアウトを変更できる", func(t *testing.T) {
		client, err := NewClient("https://host:5986/wsman", WithTimeout(30*time.Second))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		timeout := client.transport.httpClient.Timeout
		if timeout != 30*time.Second {
			t.Errorf("Timeout = %v, want 30s", timeout)
		}
	})

	t.Run("WithNTLM + WithTimeout の組み合わせ", func(t *testing.T) {
		client, err := NewClient("https://host:5986/wsman",
			WithNTLM("user", "pass"),
			WithTimeout(120*time.Second),
		)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		timeout := client.transport.httpClient.Timeout
		if timeout != 120*time.Second {
			t.Errorf("Timeout = %v, want 120s", timeout)
		}
	})

	t.Run("WithTimeout(0) はタイムアウト無制限", func(t *testing.T) {
		client, err := NewClient("https://host:5986/wsman", WithTimeout(0))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		timeout := client.transport.httpClient.Timeout
		if timeout != 0 {
			t.Errorf("Timeout = %v, want 0", timeout)
		}
	})
}

func TestWithRetry(t *testing.T) {
	t.Run("一時的な接続エラー後にリトライで成功", func(t *testing.T) {
		responseXML := loadGolden(t, "get_response_computersystem.xml")
		var attempts atomic.Int32

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			count := attempts.Add(1)
			if count <= 2 {
				// 最初の2回は接続を強制切断
				hj, ok := w.(http.Hijacker)
				if !ok {
					t.Fatal("server does not support hijacking")
				}
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			_, _ = w.Write(responseXML)
		}))
		defer server.Close()

		client, err := NewClient(server.URL,
			WithHTTPClient(server.Client()),
			WithRetry(3),
		)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		// テスト高速化のためベースディレイを短縮
		client.transport.retryBaseDelay = 10 * time.Millisecond

		resp, err := client.Get(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem")
		if err != nil {
			t.Fatalf("Get に失敗: %v", err)
		}
		if resp.Property("Name") != "SERVER01" {
			t.Errorf("Name = %q, want %q", resp.Property("Name"), "SERVER01")
		}
		if attempts.Load() != 3 {
			t.Errorf("attempts = %d, want 3", attempts.Load())
		}
	})

	t.Run("全リトライ失敗でエラーを返す", func(t *testing.T) {
		// 常に接続を拒否するリスナーを使う
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Listen failed: %v", err)
		}
		addr := listener.Addr().String()
		listener.Close() // 即座に閉じて接続拒否状態にする

		client, err := NewClient("http://"+addr+"/wsman",
			WithTimeout(500*time.Millisecond),
			WithRetry(2),
		)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		// テスト高速化のためベースディレイを短縮
		client.transport.retryBaseDelay = 10 * time.Millisecond

		_, err = client.Get(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem")
		if err == nil {
			t.Fatal("全リトライ失敗時にエラーが返されなかった")
		}
	})

	t.Run("HTTP 4xx/5xx はリトライしない", func(t *testing.T) {
		var attempts atomic.Int32

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			// ボディなしの 403 → transport はエラーとして返す
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		client, err := NewClient(server.URL,
			WithHTTPClient(server.Client()),
			WithRetry(3),
		)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, err = client.Get(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem")
		if err == nil {
			t.Fatal("403 でエラーが返されなかった")
		}
		if attempts.Load() != 1 {
			t.Errorf("attempts = %d, want 1 (リトライなし)", attempts.Load())
		}
	})

	t.Run("SOAP Fault はリトライしない", func(t *testing.T) {
		faultXML := loadGolden(t, "fault_access_denied.xml")
		var attempts atomic.Int32

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write(faultXML)
		}))
		defer server.Close()

		client, err := NewClient(server.URL,
			WithHTTPClient(server.Client()),
			WithRetry(3),
		)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, err = client.Get(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem")
		if err == nil {
			t.Fatal("Fault でエラーが返されなかった")
		}
		if attempts.Load() != 1 {
			t.Errorf("attempts = %d, want 1 (リトライなし)", attempts.Load())
		}
	})

	t.Run("リトライなし（デフォルト）の動作", func(t *testing.T) {
		var attempts atomic.Int32

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			count := attempts.Add(1)
			if count == 1 {
				hj, ok := w.(http.Hijacker)
				if !ok {
					t.Fatal("server does not support hijacking")
				}
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, err = client.Get(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem")
		if err == nil {
			t.Fatal("リトライなしで接続エラー時にエラーが返されなかった")
		}
		if attempts.Load() != 1 {
			t.Errorf("attempts = %d, want 1", attempts.Load())
		}
	})
}

func TestTransport_SendReceive(t *testing.T) {
	t.Run("正常なリクエスト・レスポンスの往復", func(t *testing.T) {
		responseXML := loadGolden(t, "get_response_computersystem.xml")

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// リクエストの検証
			if r.Method != http.MethodPost {
				t.Errorf("Method = %q, want POST", r.Method)
			}
			ct := r.Header.Get("Content-Type")
			if ct != "application/soap+xml;charset=UTF-8" {
				t.Errorf("Content-Type = %q, want application/soap+xml;charset=UTF-8", ct)
			}

			// リクエストボディの存在確認
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("リクエストボディの読み取りに失敗: %v", err)
			}
			if len(body) == 0 {
				t.Error("リクエストボディが空")
			}

			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			_, _ = w.Write(responseXML)
		}))
		defer server.Close()

		transport := NewHTTPTransport(server.URL, server.Client())
		requestXML, _ := BuildGetRequest(
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem",
			server.URL,
		)

		respData, err := transport.Send(context.Background(), requestXML)
		if err != nil {
			t.Fatalf("Send に失敗: %v", err)
		}

		if len(respData) == 0 {
			t.Error("レスポンスデータが空")
		}
	})

	t.Run("SOAP Fault レスポンスのエラーハンドリング", func(t *testing.T) {
		faultXML := loadGolden(t, "fault_access_denied.xml")

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write(faultXML)
		}))
		defer server.Close()

		transport := NewHTTPTransport(server.URL, server.Client())
		requestXML, _ := BuildGetRequest(
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_OperatingSystem",
			server.URL,
		)

		// Fault レスポンスでもデータは返す（Fault パースは呼び出し側の責任）
		respData, err := transport.Send(context.Background(), requestXML)
		if err != nil {
			t.Fatalf("Send に失敗: %v", err)
		}
		if len(respData) == 0 {
			t.Error("Fault レスポンスデータが空")
		}
	})

	t.Run("HTTP エラーの処理", func(t *testing.T) {
		// タイムアウトの短いクライアントを使用
		shortClient := &http.Client{Timeout: 100 * time.Millisecond}
		transport := NewHTTPTransport("http://192.0.2.1:5986/wsman", shortClient)
		requestXML, _ := BuildGetRequest(
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_OperatingSystem",
			"http://192.0.2.1:5986/wsman",
		)

		_, err := transport.Send(context.Background(), requestXML)
		if err == nil {
			t.Fatal("到達不能なエンドポイントでエラーが返されなかった")
		}
	})
}

// BenchmarkTransport_Send はモックサーバーに対するリクエスト送信のベンチマーク。
// Keep-Alive による接続再利用の効果を計測する。
func BenchmarkTransport_Send(b *testing.B) {
	responseXML := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body></s:Body></s:Envelope>`)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
		_, _ = w.Write(responseXML)
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, server.Client())
	requestXML, _ := BuildGetRequest(
		"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem",
		server.URL,
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := transport.Send(context.Background(), requestXML)
		if err != nil {
			b.Fatalf("Send に失敗: %v", err)
		}
	}
}
