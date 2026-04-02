package wsman

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildPutRequest(t *testing.T) {
	t.Run("基本の Put リクエスト生成", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service"
		properties := map[string]string{
			"StartMode": "Auto",
		}
		selectors := []Selector{
			{Name: "Name", Value: "TestService"},
		}

		data, err := BuildPutRequest(resourceURI, "http://host:5986/wsman", properties, selectors...)
		if err != nil {
			t.Fatalf("BuildPutRequest に失敗: %v", err)
		}

		// パースして検証
		env, err := UnmarshalEnvelope(data)
		if err != nil {
			t.Fatalf("生成された XML のパースに失敗: %v", err)
		}

		// Action が ActionPut であることを検証
		if env.Header.Action == nil || env.Header.Action.Value != ActionPut {
			t.Errorf("Action = %v, want %q", env.Header.Action, ActionPut)
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
		if env.Header.SelectorSet.Selectors[0].Name != "Name" {
			t.Errorf("Selector.Name = %q, want %q", env.Header.SelectorSet.Selectors[0].Name, "Name")
		}

		// Body にプロパティ XML が含まれることを検証
		bodyStr := string(env.Body.Content)
		if !strings.Contains(bodyStr, "Win32_Service") {
			t.Error("Body にクラス名 Win32_Service が含まれていない")
		}
		if !strings.Contains(bodyStr, "StartMode") {
			t.Error("Body にプロパティ StartMode が含まれていない")
		}
		if !strings.Contains(bodyStr, "Auto") {
			t.Error("Body にプロパティ値 Auto が含まれていない")
		}
		if !strings.Contains(bodyStr, resourceURI) {
			t.Error("Body に名前空間 URI が含まれていない")
		}
	})

	t.Run("複数プロパティの Put リクエスト", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service"
		properties := map[string]string{
			"StartMode":   "Auto",
			"DisplayName": "Updated Service",
		}

		data, err := BuildPutRequest(resourceURI, "http://host:5986/wsman", properties,
			Selector{Name: "Name", Value: "TestService"})
		if err != nil {
			t.Fatalf("BuildPutRequest に失敗: %v", err)
		}

		bodyStr := string(data)

		// 複数プロパティがソートされて出力されることを検証
		// DisplayName が StartMode より前に来る（アルファベット順）
		displayIdx := strings.Index(bodyStr, "DisplayName")
		startIdx := strings.Index(bodyStr, "StartMode")
		if displayIdx < 0 || startIdx < 0 {
			t.Fatal("Body にプロパティが含まれていない")
		}
		if displayIdx > startIdx {
			t.Error("プロパティがアルファベット順にソートされていない: DisplayName が StartMode の後に出現")
		}
	})

	t.Run("空のプロパティでエラー", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service"
		properties := map[string]string{}

		_, err := BuildPutRequest(resourceURI, "http://host:5986/wsman", properties,
			Selector{Name: "Name", Value: "TestService"})
		if err == nil {
			t.Fatal("空のプロパティでエラーが返されなかった")
		}
	})

	t.Run("nil のプロパティでエラー", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service"

		_, err := BuildPutRequest(resourceURI, "http://host:5986/wsman", nil,
			Selector{Name: "Name", Value: "TestService"})
		if err == nil {
			t.Fatal("nil のプロパティでエラーが返されなかった")
		}
	})
}

func TestParsePutResponse(t *testing.T) {
	t.Run("PutResponse からプロパティを抽出", func(t *testing.T) {
		data := loadGolden(t, "put_response_service.xml")

		resp, err := ParsePutResponse(data)
		if err != nil {
			t.Fatalf("ParsePutResponse に失敗: %v", err)
		}

		// プロパティ値を検証
		name := resp.Property("Name")
		if name != "TestService" {
			t.Errorf("Name = %q, want %q", name, "TestService")
		}

		displayName := resp.Property("DisplayName")
		if displayName != "Test Service" {
			t.Errorf("DisplayName = %q, want %q", displayName, "Test Service")
		}

		startMode := resp.Property("StartMode")
		if startMode != "Auto" {
			t.Errorf("StartMode = %q, want %q", startMode, "Auto")
		}

		state := resp.Property("State")
		if state != "Running" {
			t.Errorf("State = %q, want %q", state, "Running")
		}

		// 全プロパティ数の確認
		props := resp.Properties()
		if len(props) != 4 {
			t.Errorf("プロパティ数 = %d, want 4", len(props))
		}
	})

	t.Run("Fault レスポンスはエラーを返す", func(t *testing.T) {
		data := loadGolden(t, "fault_access_denied.xml")

		_, err := ParsePutResponse(data)
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

func TestClient_Put(t *testing.T) {
	t.Run("モックサーバーに対して Put が正しく動作", func(t *testing.T) {
		responseXML := loadGolden(t, "put_response_service.xml")

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// リクエストに Put 操作のボディが含まれることを検証
			body, _ := io.ReadAll(r.Body)
			if len(body) == 0 {
				t.Error("リクエストボディが空")
			}

			// リクエストに ActionPut が含まれることを検証
			if !strings.Contains(string(body), ActionPut) {
				t.Error("リクエストに ActionPut が含まれていない")
			}

			// リクエストに更新プロパティが含まれることを検証
			if !strings.Contains(string(body), "StartMode") {
				t.Error("リクエストにプロパティ StartMode が含まれていない")
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
			"StartMode": "Auto",
		}

		resp, err := client.Put(context.Background(),
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service",
			properties,
			Selector{Name: "Name", Value: "TestService"},
		)
		if err != nil {
			t.Fatalf("Client.Put に失敗: %v", err)
		}

		// 更新後のプロパティを検証
		name := resp.Property("Name")
		if name != "TestService" {
			t.Errorf("Name = %q, want %q", name, "TestService")
		}

		startMode := resp.Property("StartMode")
		if startMode != "Auto" {
			t.Errorf("StartMode = %q, want %q", startMode, "Auto")
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

		properties := map[string]string{
			"StartMode": "Auto",
		}

		_, err = client.Put(context.Background(),
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service",
			properties,
			Selector{Name: "Name", Value: "TestService"},
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
