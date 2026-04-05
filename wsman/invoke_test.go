package wsman

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildInvokeRequest(t *testing.T) {
	t.Run("パラメータ付き Invoke リクエスト生成", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/virtualization/v2/Msvm_VirtualSystemManagementService"
		methodName := "DefineSystem"
		params := map[string]string{
			"SystemSettings":   "<settings/>",
			"ResourceSettings": "<resources/>",
		}

		data, err := BuildInvokeRequest(resourceURI, "http://host:5986/wsman", methodName, params)
		if err != nil {
			t.Fatalf("BuildInvokeRequest に失敗: %v", err)
		}

		// パースして検証
		env, err := UnmarshalEnvelope(data)
		if err != nil {
			t.Fatalf("生成された XML のパースに失敗: %v", err)
		}

		// Action が resourceURI/methodName であることを検証
		wantAction := resourceURI + "/" + methodName
		if env.Header.Action == nil || env.Header.Action.Value != wantAction {
			t.Errorf("Action = %v, want %q", env.Header.Action, wantAction)
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

		// Body に MethodName_INPUT とパラメータ XML が含まれることを検証
		bodyStr := string(env.Body.Content)
		if !strings.Contains(bodyStr, "DefineSystem_INPUT") {
			t.Error("Body に DefineSystem_INPUT が含まれていない")
		}
		if !strings.Contains(bodyStr, resourceURI) {
			t.Error("Body に名前空間 URI が含まれていない")
		}
		if !strings.Contains(bodyStr, "SystemSettings") {
			t.Error("Body にパラメータ SystemSettings が含まれていない")
		}
		if !strings.Contains(bodyStr, "ResourceSettings") {
			t.Error("Body にパラメータ ResourceSettings が含まれていない")
		}

		// パラメータがソートされて出力されることを検証
		// ResourceSettings が SystemSettings より前に来る（アルファベット順）
		resIdx := strings.Index(bodyStr, "ResourceSettings")
		sysIdx := strings.Index(bodyStr, "SystemSettings")
		if resIdx < 0 || sysIdx < 0 {
			t.Fatal("Body にパラメータが含まれていない")
		}
		if resIdx > sysIdx {
			t.Error("パラメータがアルファベット順にソートされていない: ResourceSettings が SystemSettings の後に出現")
		}
	})

	t.Run("パラメータなし Invoke リクエスト生成", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service"
		methodName := "StartService"

		data, err := BuildInvokeRequest(resourceURI, "http://host:5986/wsman", methodName, nil)
		if err != nil {
			t.Fatalf("BuildInvokeRequest に失敗: %v", err)
		}

		// パースして検証
		env, err := UnmarshalEnvelope(data)
		if err != nil {
			t.Fatalf("生成された XML のパースに失敗: %v", err)
		}

		// Body に空の MethodName_INPUT が含まれることを検証
		bodyStr := string(env.Body.Content)
		if !strings.Contains(bodyStr, "StartService_INPUT") {
			t.Error("Body に StartService_INPUT が含まれていない")
		}

		// SelectorSet は不要（パラメータなしのクラスメソッド）
		if env.Header.SelectorSet != nil {
			t.Error("SelectorSet が設定されている（パラメータなしのクラスメソッドでは不要）")
		}
	})

	t.Run("SelectorSet 付き Invoke リクエスト", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service"
		methodName := "StartService"
		selectors := []Selector{
			{Name: "Name", Value: "Spooler"},
		}

		data, err := BuildInvokeRequest(resourceURI, "http://host:5986/wsman", methodName, nil, selectors...)
		if err != nil {
			t.Fatalf("BuildInvokeRequest に失敗: %v", err)
		}

		// パースして検証
		env, err := UnmarshalEnvelope(data)
		if err != nil {
			t.Fatalf("生成された XML のパースに失敗: %v", err)
		}

		// SelectorSet が設定されていることを検証
		if env.Header.SelectorSet == nil {
			t.Fatal("SelectorSet が nil")
		}
		if len(env.Header.SelectorSet.Selectors) != 1 {
			t.Fatalf("Selector 数 = %d, want 1", len(env.Header.SelectorSet.Selectors))
		}
		if env.Header.SelectorSet.Selectors[0].Name != "Name" {
			t.Errorf("Selector.Name = %q, want %q", env.Header.SelectorSet.Selectors[0].Name, "Name")
		}
		if env.Header.SelectorSet.Selectors[0].Value != "Spooler" {
			t.Errorf("Selector.Value = %q, want %q", env.Header.SelectorSet.Selectors[0].Value, "Spooler")
		}
	})

	t.Run("メソッド名が空でエラー", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service"

		_, err := BuildInvokeRequest(resourceURI, "http://host:5986/wsman", "", nil)
		if err == nil {
			t.Fatal("メソッド名が空でエラーが返されなかった")
		}
	})
}

func TestParseInvokeResponse(t *testing.T) {
	t.Run("ReturnValue を抽出（即時完了）", func(t *testing.T) {
		data := loadGolden(t, "invoke_response_returnvalue0.xml")

		resp, err := ParseInvokeResponse(data)
		if err != nil {
			t.Fatalf("ParseInvokeResponse に失敗: %v", err)
		}

		// ReturnValue が "0" であることを検証
		if resp.ReturnValue != "0" {
			t.Errorf("ReturnValue = %q, want %q", resp.ReturnValue, "0")
		}

		// 出力パラメータが空であることを検証（ReturnValue のみのレスポンス）
		props := resp.Properties()
		if len(props) != 0 {
			t.Errorf("出力パラメータ数 = %d, want 0", len(props))
		}
	})

	t.Run("出力パラメータ付きレスポンス", func(t *testing.T) {
		data := loadGolden(t, "invoke_response_with_output.xml")

		resp, err := ParseInvokeResponse(data)
		if err != nil {
			t.Fatalf("ParseInvokeResponse に失敗: %v", err)
		}

		// ReturnValue が "4096"（非同期ジョブ）であることを検証
		if resp.ReturnValue != "4096" {
			t.Errorf("ReturnValue = %q, want %q", resp.ReturnValue, "4096")
		}

		// Job や ResultingSystem は EndpointReference（ネスト XML）なので、
		// extractProperties は直接のテキスト子要素のみ抽出する。
		// これらはテキストノードを持たないため properties には含まれない。
		// ReturnValue だけ確実に取れれば良い。
	})

	t.Run("Fault レスポンスはエラーを返す", func(t *testing.T) {
		data := loadGolden(t, "fault_access_denied.xml")

		_, err := ParseInvokeResponse(data)
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

func TestClient_Invoke(t *testing.T) {
	t.Run("モックサーバーに対して Invoke が正しく動作", func(t *testing.T) {
		responseXML := loadGolden(t, "invoke_response_returnvalue0.xml")

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// リクエストボディを検証
			body, _ := io.ReadAll(r.Body)
			if len(body) == 0 {
				t.Error("リクエストボディが空")
			}

			// リクエストに Invoke の Action URI が含まれることを検証
			bodyStr := string(body)
			if !strings.Contains(bodyStr, "Win32_Service/StartService") {
				t.Error("リクエストに Invoke Action URI が含まれていない")
			}

			// リクエストに _INPUT 要素が含まれることを検証
			if !strings.Contains(bodyStr, "StartService_INPUT") {
				t.Error("リクエストに StartService_INPUT が含まれていない")
			}

			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			_, _ = w.Write(responseXML)
		}))
		defer server.Close()

		client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		resp, err := client.Invoke(context.Background(),
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service",
			"StartService",
			nil,
			Selector{Name: "Name", Value: "Spooler"},
		)
		if err != nil {
			t.Fatalf("Client.Invoke に失敗: %v", err)
		}

		if resp.ReturnValue != "0" {
			t.Errorf("ReturnValue = %q, want %q", resp.ReturnValue, "0")
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

		_, err = client.Invoke(context.Background(),
			"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service",
			"StartService",
			nil,
			Selector{Name: "Name", Value: "Spooler"},
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
