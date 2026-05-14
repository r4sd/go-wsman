package wsman

import (
	"testing"
)

func TestBuildEnumerateRequest(t *testing.T) {
	t.Run("基本の Enumerate リクエスト生成", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process"
		data, err := BuildEnumerateRequest(resourceURI, "http://host:5986/wsman")
		if err != nil {
			t.Fatalf("BuildEnumerateRequest に失敗: %v", err)
		}

		env, err := UnmarshalEnvelope(data)
		if err != nil {
			t.Fatalf("生成された XML のパースに失敗: %v", err)
		}

		if env.Header.Action == nil || env.Header.Action.Value != ActionEnumerate {
			t.Errorf("Action = %v, want %q", env.Header.Action, ActionEnumerate)
		}
		if env.Header.ResourceURI == nil || env.Header.ResourceURI.Value != resourceURI {
			t.Errorf("ResourceURI = %v, want %q", env.Header.ResourceURI, resourceURI)
		}

		// Body に Enumerate 要素が含まれることを確認
		if len(env.Body.Content) == 0 {
			t.Error("Body が空")
		}
	})
}

func TestBuildPullRequest(t *testing.T) {
	t.Run("Pull リクエスト生成", func(t *testing.T) {
		resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process"
		ctx := "uuid:context-001"

		data, err := BuildPullRequest(resourceURI, "http://host:5986/wsman", ctx)
		if err != nil {
			t.Fatalf("BuildPullRequest に失敗: %v", err)
		}

		env, err := UnmarshalEnvelope(data)
		if err != nil {
			t.Fatalf("生成された XML のパースに失敗: %v", err)
		}

		if env.Header.Action == nil || env.Header.Action.Value != ActionPull {
			t.Errorf("Action = %v, want %q", env.Header.Action, ActionPull)
		}

		// Body に Pull 要素と EnumerationContext が含まれることを確認
		if len(env.Body.Content) == 0 {
			t.Error("Body が空")
		}
	})
}

func TestParseEnumerateResponse(t *testing.T) {
	t.Run("EnumerationContext を抽出", func(t *testing.T) {
		data := loadGolden(t, "enumerate_response.xml")

		ctx, err := ParseEnumerateResponse(data)
		if err != nil {
			t.Fatalf("ParseEnumerateResponse に失敗: %v", err)
		}

		if ctx != "uuid:context-001" {
			t.Errorf("EnumerationContext = %q, want %q", ctx, "uuid:context-001")
		}
	})

	t.Run("Fault レスポンスはエラーを返す", func(t *testing.T) {
		data := loadGolden(t, "fault_access_denied.xml")

		_, err := ParseEnumerateResponse(data)
		if err == nil {
			t.Fatal("Fault レスポンスでエラーが返されなかった")
		}
	})
}

func TestParsePullResponse(t *testing.T) {
	t.Run("Items からインスタンスを抽出", func(t *testing.T) {
		data := loadGolden(t, "pull_response.xml")

		resp, err := ParsePullResponse(data)
		if err != nil {
			t.Fatalf("ParsePullResponse に失敗: %v", err)
		}

		if len(resp.Items) != 2 {
			t.Fatalf("Items 数 = %d, want 2", len(resp.Items))
		}

		// 1つ目のインスタンス
		name0 := resp.Items[0].Property("Name")
		if name0 != "System Idle Process" {
			t.Errorf("Items[0].Name = %q, want %q", name0, "System Idle Process")
		}
		pid0 := resp.Items[0].Property("ProcessId")
		if pid0 != "0" {
			t.Errorf("Items[0].ProcessId = %q, want %q", pid0, "0")
		}

		// 2つ目のインスタンス
		name1 := resp.Items[1].Property("Name")
		if name1 != "System" {
			t.Errorf("Items[1].Name = %q, want %q", name1, "System")
		}
	})

	t.Run("EndOfSequence の検出", func(t *testing.T) {
		data := loadGolden(t, "pull_response.xml")
		resp, err := ParsePullResponse(data)
		if err != nil {
			t.Fatalf("ParsePullResponse に失敗: %v", err)
		}
		if resp.EndOfSequence {
			t.Error("EndOfSequence = true, want false")
		}
		if resp.EnumerationContext != "uuid:context-001" {
			t.Errorf("EnumerationContext = %q, want %q", resp.EnumerationContext, "uuid:context-001")
		}
	})

	t.Run("EndOfSequence 付きレスポンス", func(t *testing.T) {
		data := loadGolden(t, "pull_response_end.xml")
		resp, err := ParsePullResponse(data)
		if err != nil {
			t.Fatalf("ParsePullResponse に失敗: %v", err)
		}
		if !resp.EndOfSequence {
			t.Error("EndOfSequence = false, want true")
		}
		if len(resp.Items) != 1 {
			t.Fatalf("Items 数 = %d, want 1", len(resp.Items))
		}
	})

	t.Run("Fault レスポンスはエラーを返す", func(t *testing.T) {
		data := loadGolden(t, "fault_access_denied.xml")

		_, err := ParsePullResponse(data)
		if err == nil {
			t.Fatal("Fault レスポンスでエラーが返されなかった")
		}
	})

	// 配列プロパティ (同名要素の繰り返し) を Instance.PropertiesList で取得できること。
	t.Run("PropertiesList で配列プロパティを取得", func(t *testing.T) {
		data := loadGolden(t, "pull_response_array.xml")
		resp, err := ParsePullResponse(data)
		if err != nil {
			t.Fatalf("ParsePullResponse に失敗: %v", err)
		}
		if len(resp.Items) != 2 {
			t.Fatalf("Items 数 = %d, want 2", len(resp.Items))
		}

		list0 := resp.Items[0].PropertiesList()
		if got := len(list0["Notes"]); got != 2 {
			t.Errorf("Items[0].Notes 要素数 = %d, want 2 (%v)", got, list0["Notes"])
		}
		if list0["Notes"][0] != "note-a1" || list0["Notes"][1] != "note-a2" {
			t.Errorf("Items[0].Notes = %v", list0["Notes"])
		}
		if list0["ElementName"][0] != "vm-a" {
			t.Errorf("Items[0].ElementName = %v", list0["ElementName"])
		}

		// Properties() (scalar) は最後の値 (後方互換)
		props0 := resp.Items[0].Properties()
		if props0["Notes"] != "note-a2" {
			t.Errorf("Items[0].Properties()[Notes] = %q, want last value %q", props0["Notes"], "note-a2")
		}
	})
}
