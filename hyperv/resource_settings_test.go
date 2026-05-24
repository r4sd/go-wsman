package hyperv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestClient_ModifyResourceSettings は Embedded Instance を 1 件送る
// ModifyResourceSettings の単体テスト。
//
// 配列パラメータ ResourceSettings が要素 1 個でも正しく XML 化されることを確認。
func TestClient_ModifyResourceSettings(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_modify_resource_settings.xml")

	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		body = string(b)
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(respXML))
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	embedded := `<p:Msvm_MemorySettingData xmlns:p="..."><p:VirtualQuantity>4096</p:VirtualQuantity></p:Msvm_MemorySettingData>`
	got, err := client.ModifyResourceSettings(context.Background(), []string{embedded})
	if err != nil {
		t.Fatalf("ModifyResourceSettings: %v", err)
	}

	if got.JobRef != "7B2A9F33-1234-5678-90AB-CDEF12345678" {
		t.Errorf("JobRef: got %q", got.JobRef)
	}
	if got.ReturnValue != "4096" {
		t.Errorf("ReturnValue: got %q", got.ReturnValue)
	}

	// リクエストボディの検証
	if !strings.Contains(body, "ModifyResourceSettings") {
		t.Errorf("body should contain method name")
	}
	if !strings.Contains(body, "<p:ResourceSettings>") {
		t.Errorf("body should contain ResourceSettings element")
	}
	if !strings.Contains(body, "VirtualQuantity") {
		t.Errorf("body should contain embedded SettingData content")
	}
}

// TestClient_ModifyResourceSettings_MultipleParams は配列パラメータが
// 複数要素でも順序通りに XML 化されることを確認する。
//
// InvokeMulti 経由で <p:ResourceSettings>...</p:ResourceSettings> が複数並ぶこと。
func TestClient_ModifyResourceSettings_MultipleParams(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_modify_resource_settings.xml")

	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(respXML))
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	settings := []string{
		`<p:Foo xmlns:p="x">FIRST</p:Foo>`,
		`<p:Bar xmlns:p="x">SECOND</p:Bar>`,
		`<p:Baz xmlns:p="x">THIRD</p:Baz>`,
	}
	if _, err := client.ModifyResourceSettings(context.Background(), settings); err != nil {
		t.Fatalf("ModifyResourceSettings: %v", err)
	}

	// 順序を確認: FIRST → SECOND → THIRD の出現順
	idxFirst := strings.Index(body, "FIRST")
	idxSecond := strings.Index(body, "SECOND")
	idxThird := strings.Index(body, "THIRD")
	if idxFirst < 0 || idxSecond < 0 || idxThird < 0 {
		t.Fatalf("body missing markers: first=%d second=%d third=%d", idxFirst, idxSecond, idxThird)
	}
	if idxFirst >= idxSecond || idxSecond >= idxThird {
		t.Errorf("array params are out of order: first=%d second=%d third=%d", idxFirst, idxSecond, idxThird)
	}

	// ResourceSettings タグが3個出現する
	if got := strings.Count(body, "<p:ResourceSettings>"); got != 3 {
		t.Errorf("<p:ResourceSettings> count: got %d, want 3", got)
	}
}

// TestClient_ModifyResourceSettings_Empty はバリデーション。
func TestClient_ModifyResourceSettings_Empty(t *testing.T) {
	client, _ := NewClient("http://localhost")
	if _, err := client.ModifyResourceSettings(context.Background(), nil); err == nil {
		t.Error("expected error for empty settings")
	}
}

// TestClient_RemoveResourceSettings は EPR を渡して削除する流れの単体テスト。
func TestClient_RemoveResourceSettings(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_remove_resource_settings.xml")

	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(respXML))
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)

	epr := buildEndpointReference(msvmMemorySettingDataURI, map[string]string{
		"InstanceID": "Microsoft:vm-guid\\NIC-001",
	})
	jobRef, err := client.RemoveResourceSettings(context.Background(), []string{epr})
	if err != nil {
		t.Fatalf("RemoveResourceSettings: %v", err)
	}
	if jobRef != "9C8B7A66-FFEE-DDCC-BBAA-998877665544" {
		t.Errorf("jobRef: got %q", jobRef)
	}
	if !strings.Contains(body, "RemoveResourceSettings") {
		t.Errorf("body should contain method name")
	}
	if !strings.Contains(body, "EndpointReference") {
		t.Errorf("body should contain EPR")
	}
}

// TestClient_RemoveResourceSettings_Empty はバリデーション。
func TestClient_RemoveResourceSettings_Empty(t *testing.T) {
	client, _ := NewClient("http://localhost")
	if _, err := client.RemoveResourceSettings(context.Background(), nil); err == nil {
		t.Error("expected error for empty resourceRefs")
	}
}

// TestClient_AddResourceSettings は GetSystemSettingData → AddResourceSettings の
// 2 段階フローを検証する。
//
// httptest server で 3 リクエストを順に処理:
//
//	count==1: Enumerate (WQL for VirtualSystemSettingData)
//	count==2: Pull (Realized SettingData を返す)
//	count==3: AddResourceSettings Invoke
func TestClient_AddResourceSettings(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_systemsettingdata.xml")
	pullXML := loadGolden(t, "pull_response_systemsettingdata.xml")
	invokeXML := loadGolden(t, "invoke_response_add_resource_settings.xml")

	var invokeBody string
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		callCount++
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		switch callCount {
		case 1:
			_, _ = w.Write([]byte(enumXML))
		case 2:
			_, _ = w.Write([]byte(pullXML))
		default:
			invokeBody = string(b)
			_, _ = w.Write([]byte(invokeXML))
		}
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)

	embedded := `<p:Msvm_SyntheticEthernetPortSettingData xmlns:p="..."><p:ElementName>NIC1</p:ElementName></p:Msvm_SyntheticEthernetPortSettingData>`
	got, err := client.AddResourceSettings(context.Background(),
		"11111111-aaaa-bbbb-cccc-000000000001", []string{embedded})
	if err != nil {
		t.Fatalf("AddResourceSettings: %v", err)
	}

	if got.JobRef != "3A1B2C44-1111-2222-3333-AABBCCDDEEFF" {
		t.Errorf("JobRef: got %q", got.JobRef)
	}

	// AffectedConfiguration が EPR で送信されていること
	if !strings.Contains(invokeBody, "AffectedConfiguration") {
		t.Errorf("invoke body should contain AffectedConfiguration")
	}
	if !strings.Contains(invokeBody, "Msvm_VirtualSystemSettingData") {
		t.Errorf("invoke body EPR should reference Msvm_VirtualSystemSettingData")
	}
	if !strings.Contains(invokeBody, "ResourceSettings") {
		t.Errorf("invoke body should contain ResourceSettings")
	}
}

// TestClient_AddResourceSettings_Validation
func TestClient_AddResourceSettings_Validation(t *testing.T) {
	client, _ := NewClient("http://localhost")
	if _, err := client.AddResourceSettings(context.Background(), "", []string{"x"}); err == nil {
		t.Error("expected error for empty vmName")
	}
	if _, err := client.AddResourceSettings(context.Background(), "vm", nil); err == nil {
		t.Error("expected error for empty settings")
	}
}
