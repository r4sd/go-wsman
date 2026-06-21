package hyperv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestClient_GetMemorySettings は VM GUID からメモリ設定を取得するテスト。
//
// Hyper-V は WQL フィルタ列挙を拒否する (#80) ため、無フィルタ列挙 + Go 側の InstanceID
// prefix フィルタで対象 VM のメモリ設定だけを選ぶ。multi golden には別 VM
// (8192MB/動的メモリ有効) のメモリ設定も含めて、対象 VM (2048MB) を正しく選べることを検証する。
func TestClient_GetMemorySettings(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_memorysettingdata.xml")
	pullXML := loadGolden(t, "pull_response_memorysettingdata_multi.xml")

	var enumBody string
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		callCount++
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		if callCount == 1 {
			enumBody = string(b)
			_, _ = w.Write([]byte(enumXML))
		} else {
			_, _ = w.Write([]byte(pullXML))
		}
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)

	got, err := client.GetMemorySettings(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("GetMemorySettings: %v", err)
	}

	// 対象 VM のメモリ設定 (2048MB) が選ばれること (別 VM の 8192MB ではない)。
	if got.VirtualQuantity != 2048 {
		t.Errorf("VirtualQuantity: got %d, want 2048 (別 VM の設定を選んではいけない)", got.VirtualQuantity)
	}
	if got.ResourceType != ResourceTypeMemory {
		t.Errorf("ResourceType: got %d, want %d", got.ResourceType, ResourceTypeMemory)
	}
	if got.DynamicMemoryEnabled {
		t.Errorf("DynamicMemoryEnabled: want false")
	}
	if got.Weight != 5000 {
		t.Errorf("Weight: got %d, want 5000", got.Weight)
	}

	// Hyper-V は WQL フィルタ列挙を拒否するため、Enumerate は無フィルタで送ること (#80)。
	if strings.Contains(enumBody, "Filter") || strings.Contains(enumBody, "SELECT") {
		t.Errorf("enum body should be unfiltered (no WQL Filter); body: %s", enumBody)
	}
}

// TestClient_GetMemorySettings_NotFound はマッチがない場合のエラー。
func TestClient_GetMemorySettings_NotFound(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_memorysettingdata.xml")
	emptyPull := `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:e="http://schemas.xmlsoap.org/ws/2004/09/enumeration">
  <s:Body>
    <e:PullResponse><e:Items/><e:EndOfSequence/></e:PullResponse>
  </s:Body>
</s:Envelope>`

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		if callCount == 1 {
			_, _ = w.Write([]byte(enumXML))
		} else {
			_, _ = w.Write([]byte(emptyPull))
		}
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	if _, err := client.GetMemorySettings(context.Background(), "missing-vm"); err == nil {
		t.Fatal("expected NotFound error")
	}
}

// TestClient_SetMemorySettings は ModifyResourceSettings 経由で送られることを検証する。
func TestClient_SetMemorySettings(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_modify_resource_settings.xml")

	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(respXML))
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)

	settings := &Msvm_MemorySettingData{
		InstanceID:      "Microsoft:vm-guid\\F4DA67D7",
		VirtualQuantity: 4096,
		Weight:          5000,
	}
	jobRef, err := client.SetMemorySettings(context.Background(), settings)
	if err != nil {
		t.Fatalf("SetMemorySettings: %v", err)
	}
	if jobRef == "" {
		t.Error("jobRef should not be empty")
	}

	// 送られた XML が Msvm_MemorySettingData を含むこと
	if !strings.Contains(body, "Msvm_MemorySettingData") {
		t.Errorf("body should contain Msvm_MemorySettingData embedded class")
	}
	// embedded instance は CIM-XML <PROPERTY> 形式で CDATA 内に入る (#81)。
	if !strings.Contains(body, `<PROPERTY NAME="VirtualQuantity" TYPE="uint64"><VALUE>4096</VALUE></PROPERTY>`) {
		t.Errorf("body should contain new VirtualQuantity value")
	}
	if !strings.Contains(body, "ModifyResourceSettings") {
		t.Errorf("body should call ModifyResourceSettings")
	}
}

// TestClient_SetMemorySettings_Validation
func TestClient_SetMemorySettings_Validation(t *testing.T) {
	client, _ := NewClient("http://localhost")
	if _, err := client.SetMemorySettings(context.Background(), nil); err == nil {
		t.Error("expected error for nil settings")
	}
	if _, err := client.SetMemorySettings(context.Background(), &Msvm_MemorySettingData{}); err == nil {
		t.Error("expected error for empty InstanceID")
	}
}

// TestClient_GetProcessorSettings は CPU 設定を WQL 経由で取得する。
func TestClient_GetProcessorSettings(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_processorsettingdata.xml")
	pullXML := loadGolden(t, "pull_response_processorsettingdata.xml")

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		if callCount == 1 {
			_, _ = w.Write([]byte(enumXML))
		} else {
			_, _ = w.Write([]byte(pullXML))
		}
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)

	got, err := client.GetProcessorSettings(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("GetProcessorSettings: %v", err)
	}

	if got.VirtualQuantity != 2 {
		t.Errorf("VirtualQuantity: got %d, want 2", got.VirtualQuantity)
	}
	if got.ResourceType != ResourceTypeProcessor {
		t.Errorf("ResourceType: got %d, want %d", got.ResourceType, ResourceTypeProcessor)
	}
	if got.Limit != 100000 {
		t.Errorf("Limit: got %d, want 100000", got.Limit)
	}
	if got.ExposeVirtualizationExtensions {
		t.Errorf("ExposeVirtualizationExtensions: want false")
	}
}

// TestClient_SetProcessorSettings
func TestClient_SetProcessorSettings(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_modify_resource_settings.xml")

	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(respXML))
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)

	settings := &Msvm_ProcessorSettingData{
		InstanceID:                     "Microsoft:vm-guid\\509E3A23",
		VirtualQuantity:                4,
		ExposeVirtualizationExtensions: true,
	}
	if _, err := client.SetProcessorSettings(context.Background(), settings); err != nil {
		t.Fatalf("SetProcessorSettings: %v", err)
	}

	if !strings.Contains(body, "Msvm_ProcessorSettingData") {
		t.Errorf("body should contain Msvm_ProcessorSettingData embedded class")
	}
	// embedded instance は CIM-XML <PROPERTY> 形式で CDATA 内に入る (#81)。
	if !strings.Contains(body, `<PROPERTY NAME="VirtualQuantity" TYPE="uint64"><VALUE>4</VALUE></PROPERTY>`) {
		t.Errorf("body should contain new VirtualQuantity")
	}
	// bool は CIM-XML 標準 (DSP0201) の小文字 true。
	if !strings.Contains(body, `<PROPERTY NAME="ExposeVirtualizationExtensions" TYPE="boolean"><VALUE>true</VALUE></PROPERTY>`) {
		t.Errorf("body should contain ExposeVirtualizationExtensions=true")
	}
}
