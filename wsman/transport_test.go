package wsman

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
			w.Write(responseXML)
		}))
		defer server.Close()

		transport := NewHTTPTransport(server.URL, server.Client())
		requestXML, _ := BuildGetRequest(
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem",
			server.URL,
		)

		respData, err := transport.Send(requestXML)
		if err != nil {
			t.Fatalf("Send に失敗: %v", err)
		}

		if len(respData) == 0 {
			t.Error("レスポンスデータが空")
		}
	})

	t.Run("SOAP Fault レスポンスのエラーハンドリング", func(t *testing.T) {
		faultXML := loadGolden(t, "fault_access_denied.xml")

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write(faultXML)
		}))
		defer server.Close()

		transport := NewHTTPTransport(server.URL, server.Client())
		requestXML, _ := BuildGetRequest(
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_OperatingSystem",
			server.URL,
		)

		// Fault レスポンスでもデータは返す（Fault パースは呼び出し側の責任）
		respData, err := transport.Send(requestXML)
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

		_, err := transport.Send(requestXML)
		if err == nil {
			t.Fatal("到達不能なエンドポイントでエラーが返されなかった")
		}
	})
}
