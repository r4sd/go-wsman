package wsman

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildEnumerateRequest_WithWQL(t *testing.T) {
	t.Run("WQL フィルタ付き Enumerate リクエスト生成", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service"
		wql := "SELECT * FROM Win32_Service WHERE State = 'Running'"

		data, err := BuildEnumerateRequest(resourceURI, "http://host:5986/wsman", WithWQL(wql))
		if err != nil {
			t.Fatalf("BuildEnumerateRequest に失敗: %v", err)
		}

		// パースして検証
		env, err := UnmarshalEnvelope(data)
		if err != nil {
			t.Fatalf("生成された XML のパースに失敗: %v", err)
		}

		// Action が ActionEnumerate であることを検証
		if env.Header.Action == nil || env.Header.Action.Value != ActionEnumerate {
			t.Errorf("Action = %v, want %q", env.Header.Action, ActionEnumerate)
		}

		// ResourceURI
		if env.Header.ResourceURI == nil || env.Header.ResourceURI.Value != resourceURI {
			t.Errorf("ResourceURI = %v, want %q", env.Header.ResourceURI, resourceURI)
		}

		// Body に Filter 要素と WQL Dialect が含まれることを検証
		bodyStr := string(data)
		if !strings.Contains(bodyStr, DialectWQL) {
			t.Error("Body に WQL Dialect URI が含まれていない")
		}
		if !strings.Contains(bodyStr, wql) {
			t.Error("Body に WQL クエリが含まれていない")
		}
		if !strings.Contains(bodyStr, "w:Filter") {
			t.Error("Body に w:Filter 要素が含まれていない")
		}
	})

	t.Run("WQL なしの Enumerate リクエストは従来通り", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process"

		data, err := BuildEnumerateRequest(resourceURI, "http://host:5986/wsman")
		if err != nil {
			t.Fatalf("BuildEnumerateRequest に失敗: %v", err)
		}

		bodyStr := string(data)
		if strings.Contains(bodyStr, "Filter") {
			t.Error("フィルタなしのリクエストに Filter 要素が含まれている")
		}
	})

	t.Run("空の WQL クエリはエラー", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service"

		_, err := BuildEnumerateRequest(resourceURI, "http://host:5986/wsman", WithWQL(""))
		if err == nil {
			t.Fatal("空の WQL クエリでエラーが返されなかった")
		}
	})
}

func TestClient_Enumerate_WithWQL(t *testing.T) {
	t.Run("WQL フィルタ付きで Enumerate が正しく動作", func(t *testing.T) {
		// Enumerate → Pull の2段階のレスポンス
		callCount := 0
		enumerateResponseXML := loadGolden(t, "enumerate_response.xml")
		pullResponseXML := loadGolden(t, "pull_response_end.xml")

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			bodyStr := string(body)

			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")

			if callCount == 0 {
				// Enumerate リクエスト
				if !strings.Contains(bodyStr, ActionEnumerate) {
					t.Error("最初のリクエストに ActionEnumerate が含まれていない")
				}
				// WQL フィルタが含まれることを検証
				if !strings.Contains(bodyStr, "Filter") {
					t.Error("Enumerate リクエストに Filter 要素が含まれていない")
				}
				if !strings.Contains(bodyStr, DialectWQL) {
					t.Error("Enumerate リクエストに WQL Dialect が含まれていない")
				}
				if !strings.Contains(bodyStr, "SELECT") {
					t.Error("Enumerate リクエストに WQL クエリが含まれていない")
				}
				_, _ = w.Write(enumerateResponseXML)
			} else {
				// Pull リクエスト
				if !strings.Contains(bodyStr, ActionPull) {
					t.Error("2回目のリクエストに ActionPull が含まれていない")
				}
				_, _ = w.Write(pullResponseXML)
			}
			callCount++
		}))
		defer server.Close()

		client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		instances, err := client.Enumerate(context.Background(),
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service",
			WithWQL("SELECT * FROM Win32_Service WHERE State = 'Running'"),
		)
		if err != nil {
			t.Fatalf("Client.Enumerate に失敗: %v", err)
		}

		if len(instances) != 1 {
			t.Errorf("インスタンス数 = %d, want 1", len(instances))
		}

		// 2回呼ばれたことを検証（Enumerate + Pull）
		if callCount != 2 {
			t.Errorf("サーバー呼び出し回数 = %d, want 2", callCount)
		}
	})

	t.Run("WQL なしで従来の Enumerate が正しく動作", func(t *testing.T) {
		callCount := 0
		enumerateResponseXML := loadGolden(t, "enumerate_response.xml")
		pullResponseXML := loadGolden(t, "pull_response_end.xml")

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			bodyStr := string(body)

			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")

			if callCount == 0 {
				// フィルタなしの Enumerate リクエスト
				if strings.Contains(bodyStr, "Filter") {
					t.Error("フィルタなしリクエストに Filter 要素が含まれている")
				}
				_, _ = w.Write(enumerateResponseXML)
			} else {
				_, _ = w.Write(pullResponseXML)
			}
			callCount++
		}))
		defer server.Close()

		client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		// オプションなしで呼び出し（後方互換性の確認）
		instances, err := client.Enumerate(context.Background(),
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process",
		)
		if err != nil {
			t.Fatalf("Client.Enumerate に失敗: %v", err)
		}

		if len(instances) != 1 {
			t.Errorf("インスタンス数 = %d, want 1", len(instances))
		}
	})
}
