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

	// 配列プロパティ (同名要素の繰り返し) を扱えること。
	// CIM の string[] / uint16[] 等はレスポンス XML で同じ要素名が複数回出現する。
	t.Run("PropertiesList で配列プロパティを取得", func(t *testing.T) {
		data := loadGolden(t, "get_response_vsetting_array.xml")

		resp, err := ParseGetResponse(data)
		if err != nil {
			t.Fatalf("ParseGetResponse に失敗: %v", err)
		}

		list := resp.PropertiesList()
		if got := len(list["Notes"]); got != 3 {
			t.Errorf("Notes 要素数 = %d, want 3 (%v)", got, list["Notes"])
		}
		for i, want := range []string{"created by terraform", "managed", "line3"} {
			if list["Notes"][i] != want {
				t.Errorf("Notes[%d] = %q, want %q", i, list["Notes"][i], want)
			}
		}
		if got := len(list["BootSourceOrder"]); got != 2 {
			t.Errorf("BootSourceOrder 要素数 = %d, want 2", got)
		}
		if list["ElementName"][0] != "my-vm" {
			t.Errorf("ElementName = %v", list["ElementName"])
		}
	})

	// Properties() (scalar map) は最後の値を返す (既存挙動を維持)。
	t.Run("Properties は配列の最後の値を返す (後方互換)", func(t *testing.T) {
		data := loadGolden(t, "get_response_vsetting_array.xml")

		resp, err := ParseGetResponse(data)
		if err != nil {
			t.Fatalf("ParseGetResponse に失敗: %v", err)
		}

		props := resp.Properties()
		if props["Notes"] != "line3" {
			t.Errorf("Notes (scalar) = %q, want %q (last value)", props["Notes"], "line3")
		}
		if props["ElementName"] != "my-vm" {
			t.Errorf("ElementName = %q", props["ElementName"])
		}
	})
}
