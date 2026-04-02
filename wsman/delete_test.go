package wsman

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildDeleteRequest(t *testing.T) {
	t.Run("基本の Delete リクエスト生成", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process"
		selectors := []Selector{
			{Name: "Handle", Value: "12345"},
		}

		data, err := BuildDeleteRequest(resourceURI, "http://host:5986/wsman", selectors...)
		if err != nil {
			t.Fatalf("BuildDeleteRequest に失敗: %v", err)
		}

		// パースして検証
		env, err := UnmarshalEnvelope(data)
		if err != nil {
			t.Fatalf("生成された XML のパースに失敗: %v", err)
		}

		// Action が ActionDelete であることを検証
		if env.Header.Action == nil || env.Header.Action.Value != ActionDelete {
			t.Errorf("Action = %v, want %q", env.Header.Action, ActionDelete)
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

		// SelectorSet
		if env.Header.SelectorSet == nil {
			t.Fatal("SelectorSet が nil")
		}
		if len(env.Header.SelectorSet.Selectors) != 1 {
			t.Fatalf("Selector 数 = %d, want 1", len(env.Header.SelectorSet.Selectors))
		}
		if env.Header.SelectorSet.Selectors[0].Name != "Handle" {
			t.Errorf("Selector.Name = %q, want %q", env.Header.SelectorSet.Selectors[0].Name, "Handle")
		}
		if env.Header.SelectorSet.Selectors[0].Value != "12345" {
			t.Errorf("Selector.Value = %q, want %q", env.Header.SelectorSet.Selectors[0].Value, "12345")
		}

		// Body が空であることを検証（Delete はボディなし）
		bodyStr := strings.TrimSpace(string(env.Body.Content))
		if bodyStr != "" {
			t.Errorf("Body が空ではない: %q", bodyStr)
		}
	})
}

func TestParseDeleteResponse(t *testing.T) {
	t.Run("正常レスポンスでエラーなし", func(t *testing.T) {
		data := loadGolden(t, "delete_response.xml")

		err := ParseDeleteResponse(data)
		if err != nil {
			t.Fatalf("ParseDeleteResponse がエラーを返した: %v", err)
		}
	})

	t.Run("Fault レスポンスはエラーを返す", func(t *testing.T) {
		data := loadGolden(t, "fault_access_denied.xml")

		err := ParseDeleteResponse(data)
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

func TestClient_Delete(t *testing.T) {
	t.Run("モックサーバーに対して Delete が正しく動作", func(t *testing.T) {
		responseXML := loadGolden(t, "delete_response.xml")

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// リクエストに Delete 操作のボディが含まれることを検証
			body, _ := io.ReadAll(r.Body)
			if len(body) == 0 {
				t.Error("リクエストボディが空")
			}

			// リクエストに ActionDelete が含まれることを検証
			if !strings.Contains(string(body), ActionDelete) {
				t.Error("リクエストに ActionDelete が含まれていない")
			}

			// リクエストに SelectorSet が含まれることを検証
			if !strings.Contains(string(body), "Handle") {
				t.Error("リクエストに Selector Handle が含まれていない")
			}

			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			_, _ = w.Write(responseXML)
		}))
		defer server.Close()

		client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		err = client.Delete(context.Background(),
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process",
			Selector{Name: "Handle", Value: "12345"},
		)
		if err != nil {
			t.Fatalf("Client.Delete に失敗: %v", err)
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

		err = client.Delete(context.Background(),
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process",
			Selector{Name: "Handle", Value: "12345"},
		)
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
