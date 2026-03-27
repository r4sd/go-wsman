package wsman

import (
	"testing"
)

func TestBuildGetRequest(t *testing.T) {
	t.Run("基本の Get リクエスト生成", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_OperatingSystem"
		data, err := BuildGetRequest(resourceURI, "http://host:5986/wsman")
		if err != nil {
			t.Fatalf("BuildGetRequest に失敗: %v", err)
		}

		// パースして検証
		env, err := UnmarshalEnvelope(data)
		if err != nil {
			t.Fatalf("生成された XML のパースに失敗: %v", err)
		}

		if env.Header.Action == nil || env.Header.Action.Value != ActionGet {
			t.Errorf("Action = %v, want %q", env.Header.Action, ActionGet)
		}
		if env.Header.ResourceURI == nil || env.Header.ResourceURI.Value != resourceURI {
			t.Errorf("ResourceURI = %v, want %q", env.Header.ResourceURI, resourceURI)
		}
		if env.Header.To == nil || env.Header.To.Value != "http://host:5986/wsman" {
			t.Errorf("To = %v, want %q", env.Header.To, "http://host:5986/wsman")
		}
		if env.Header.ReplyTo == nil || env.Header.ReplyTo.Address.Value != AddressAnonymous {
			t.Error("ReplyTo が正しく設定されていない")
		}
		if env.Header.MessageID == nil || env.Header.MessageID.Value == "" {
			t.Error("MessageID が設定されていない")
		}
	})

	t.Run("SelectorSet 付き Get リクエスト", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service"
		selectors := []Selector{
			{Name: "Name", Value: "WinRM"},
		}
		data, err := BuildGetRequest(resourceURI, "http://host:5986/wsman", selectors...)
		if err != nil {
			t.Fatalf("BuildGetRequest に失敗: %v", err)
		}

		env, err := UnmarshalEnvelope(data)
		if err != nil {
			t.Fatalf("生成された XML のパースに失敗: %v", err)
		}

		if env.Header.SelectorSet == nil {
			t.Fatal("SelectorSet が nil")
		}
		if len(env.Header.SelectorSet.Selectors) != 1 {
			t.Fatalf("Selector 数 = %d, want 1", len(env.Header.SelectorSet.Selectors))
		}
		if env.Header.SelectorSet.Selectors[0].Name != "Name" {
			t.Errorf("Selector.Name = %q, want %q", env.Header.SelectorSet.Selectors[0].Name, "Name")
		}
	})
}

func TestParseGetResponse(t *testing.T) {
	t.Run("ComputerSystem レスポンスからプロパティを抽出", func(t *testing.T) {
		data := loadGolden(t, "get_response_computersystem.xml")

		resp, err := ParseGetResponse(data)
		if err != nil {
			t.Fatalf("ParseGetResponse に失敗: %v", err)
		}

		name := resp.Property("Name")
		if name != "SERVER01" {
			t.Errorf("Name = %q, want %q", name, "SERVER01")
		}

		domain := resp.Property("Domain")
		if domain != "WORKGROUP" {
			t.Errorf("Domain = %q, want %q", domain, "WORKGROUP")
		}

		manufacturer := resp.Property("Manufacturer")
		if manufacturer != "HP" {
			t.Errorf("Manufacturer = %q, want %q", manufacturer, "HP")
		}

		totalMem := resp.Property("TotalPhysicalMemory")
		if totalMem != "68719476736" {
			t.Errorf("TotalPhysicalMemory = %q, want %q", totalMem, "68719476736")
		}
	})

	t.Run("存在しないプロパティは空文字列", func(t *testing.T) {
		data := loadGolden(t, "get_response_computersystem.xml")

		resp, err := ParseGetResponse(data)
		if err != nil {
			t.Fatalf("ParseGetResponse に失敗: %v", err)
		}

		val := resp.Property("NonExistent")
		if val != "" {
			t.Errorf("NonExistent プロパティ = %q, want 空文字列", val)
		}
	})

	t.Run("Properties で全プロパティを取得", func(t *testing.T) {
		data := loadGolden(t, "get_response_computersystem.xml")

		resp, err := ParseGetResponse(data)
		if err != nil {
			t.Fatalf("ParseGetResponse に失敗: %v", err)
		}

		props := resp.Properties()
		if len(props) != 6 {
			t.Errorf("プロパティ数 = %d, want 6", len(props))
		}
	})

	t.Run("Fault レスポンスはエラーを返す", func(t *testing.T) {
		data := loadGolden(t, "fault_access_denied.xml")

		_, err := ParseGetResponse(data)
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
