package hyperv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newSequenceServer は callCount に応じた応答を順に返す httptest server を作る。
// 各リクエストのボディは bodies スライスに記録される。
func newSequenceServer(t *testing.T, responses []string, bodies *[]string) *httptest.Server {
	t.Helper()
	count := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		*bodies = append(*bodies, string(body))
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		if count < len(responses) {
			_, _ = w.Write([]byte(responses[count]))
		}
		count++
	}))
}

// TestClient_ListVirtualEthernetSwitches は仮想スイッチ一覧の取得を検証する。
func TestClient_ListVirtualEthernetSwitches(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_virtualethernetswitch.xml")
	pull := loadGolden(t, "pull_response_virtualethernetswitch.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListVirtualEthernetSwitches(context.Background())
	if err != nil {
		t.Fatalf("ListVirtualEthernetSwitches: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len: got %d, want 2", len(got))
	}
	if got[0].ElementName != "External" {
		t.Errorf("got[0].ElementName: %q", got[0].ElementName)
	}
	if got[1].ElementName != "Internal" {
		t.Errorf("got[1].ElementName: %q", got[1].ElementName)
	}
	if got[0].HealthState != 5 {
		t.Errorf("got[0].HealthState: %d", got[0].HealthState)
	}
}

// TestClient_GetVirtualEthernetSwitch は ElementName で絞り込めることを検証する。
func TestClient_GetVirtualEthernetSwitch(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_virtualethernetswitch.xml")
	pull := loadGolden(t, "pull_response_virtualethernetswitch.xml")

	t.Run("found", func(t *testing.T) {
		var bodies []string
		server := newSequenceServer(t, []string{enum, pull}, &bodies)
		defer server.Close()

		client, _ := NewClient(server.URL)
		sw, err := client.GetVirtualEthernetSwitch(context.Background(), "External")
		if err != nil {
			t.Fatalf("GetVirtualEthernetSwitch: %v", err)
		}
		if sw.Name != "AAAAAAAA-1111-1111-1111-AAAAAAAAAAAA" {
			t.Errorf("Name: %q", sw.Name)
		}
	})

	t.Run("not found", func(t *testing.T) {
		var bodies []string
		server := newSequenceServer(t, []string{enum, pull}, &bodies)
		defer server.Close()

		client, _ := NewClient(server.URL)
		_, err := client.GetVirtualEthernetSwitch(context.Background(), "NonExistent")
		if err == nil {
			t.Error("expected error for non-existent switch")
		}
	})

	t.Run("empty name", func(t *testing.T) {
		client, _ := NewClient("http://localhost")
		if _, err := client.GetVirtualEthernetSwitch(context.Background(), ""); err == nil {
			t.Error("expected error for empty name")
		}
	})
}

// TestClient_ListNetworkAdapters は VM の NIC 一覧取得を検証する。
func TestClient_ListNetworkAdapters(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_syntheticethernetport.xml")
	pull := loadGolden(t, "pull_response_syntheticethernetport.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListNetworkAdapters(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("ListNetworkAdapters: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	if got[0].Address != "00155D012345" {
		t.Errorf("Address: %q", got[0].Address)
	}
	if got[0].ResourceType != ResourceTypeEthernetAdapter {
		t.Errorf("ResourceType: got %d, want %d", got[0].ResourceType, ResourceTypeEthernetAdapter)
	}

	// WQL に VM GUID と LIKE が含まれること
	if !strings.Contains(bodies[0], "11111111-aaaa-bbbb-cccc-000000000001") {
		t.Errorf("enumerate body should contain VM GUID")
	}
}

// TestClient_AddNetworkAdapter_NoSwitch は SwitchName 未指定の場合、
// NIC 本体のみ追加される (スイッチ接続の AddResourceSettings は呼ばない) ことを検証する。
//
// 想定リクエスト順:
//
//  1. enumerate (system setting data, AddResourceSettings 内部の GetSystemSettingData)
//  2. pull (system setting data)
//  3. invoke (AddResourceSettings, NIC 本体)
func TestClient_AddNetworkAdapter_NoSwitch(t *testing.T) {
	sysEnum := loadGolden(t, "enumerate_response_systemsettingdata.xml")
	sysPull := loadGolden(t, "pull_response_systemsettingdata.xml")
	addResp := loadGolden(t, "invoke_response_add_resource_settings.xml")

	var bodies []string
	server := newSequenceServer(t, []string{sysEnum, sysPull, addResp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.AddNetworkAdapter(context.Background(),
		"11111111-aaaa-bbbb-cccc-000000000001",
		NetworkAdapterOptions{
			ElementName: "NIC1",
		})
	if err != nil {
		t.Fatalf("AddNetworkAdapter: %v", err)
	}

	if got.PortRef == "" {
		t.Errorf("PortRef should not be empty")
	}
	if got.AllocationRef != "" {
		t.Errorf("AllocationRef should be empty when no switch specified, got %q", got.AllocationRef)
	}

	// 全 3 リクエスト
	if len(bodies) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(bodies))
	}

	// 3 番目のリクエスト (Invoke) に NIC 関連のフィールドが含まれること
	invokeBody := bodies[2]
	if !strings.Contains(invokeBody, "AddResourceSettings") {
		t.Errorf("invoke body should call AddResourceSettings")
	}
	if !strings.Contains(invokeBody, "Msvm_SyntheticEthernetPortSettingData") {
		t.Errorf("invoke body should contain Msvm_SyntheticEthernetPortSettingData")
	}
	if !strings.Contains(invokeBody, "NIC1") {
		t.Errorf("invoke body should contain NIC element name")
	}
	if !strings.Contains(invokeBody, ResourceSubTypeSyntheticEthernetPort) {
		t.Errorf("invoke body should contain ResourceSubType")
	}
}

// TestClient_AddNetworkAdapter_WithSwitch は SwitchName 指定の場合、
// NIC 本体追加 + スイッチ接続の 2 段階 AddResourceSettings が走ることを検証する。
//
// 想定リクエスト順 (8 件):
//
//	1-3: AddResourceSettings (NIC 本体)
//	4-5: ListVirtualEthernetSwitches (enumerate + pull)
//	6-8: AddResourceSettings (スイッチ接続)
func TestClient_AddNetworkAdapter_WithSwitch(t *testing.T) {
	sysEnum := loadGolden(t, "enumerate_response_systemsettingdata.xml")
	sysPull := loadGolden(t, "pull_response_systemsettingdata.xml")
	addResp := loadGolden(t, "invoke_response_add_resource_settings.xml")
	swEnum := loadGolden(t, "enumerate_response_virtualethernetswitch.xml")
	swPull := loadGolden(t, "pull_response_virtualethernetswitch.xml")

	responses := []string{
		sysEnum, sysPull, addResp, // NIC 本体追加
		swEnum, swPull, // スイッチ取得
		sysEnum, sysPull, addResp, // スイッチ接続追加
	}

	var bodies []string
	server := newSequenceServer(t, responses, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.AddNetworkAdapter(context.Background(),
		"11111111-aaaa-bbbb-cccc-000000000001",
		NetworkAdapterOptions{
			ElementName: "NIC1",
			SwitchName:  "External",
		})
	if err != nil {
		t.Fatalf("AddNetworkAdapter: %v", err)
	}

	if got.PortRef == "" {
		t.Errorf("PortRef should not be empty")
	}
	if got.AllocationRef == "" {
		t.Errorf("AllocationRef should not be empty when switch attached")
	}

	if len(bodies) != 8 {
		t.Fatalf("expected 8 requests, got %d", len(bodies))
	}

	// 8 番目 (allocation invoke) に EthernetPortAllocation 関連が含まれる
	allocBody := bodies[7]
	if !strings.Contains(allocBody, "Msvm_EthernetPortAllocationSettingData") {
		t.Errorf("allocation body should contain Msvm_EthernetPortAllocationSettingData")
	}
	if !strings.Contains(allocBody, ResourceSubTypeEthernetConnection) {
		t.Errorf("allocation body should contain Ethernet Connection ResourceSubType")
	}
	// HostResource にスイッチ EPR が埋め込まれていること (External の Name)
	if !strings.Contains(allocBody, "AAAAAAAA-1111-1111-1111-AAAAAAAAAAAA") {
		t.Errorf("allocation body should reference External switch GUID")
	}
}

// TestClient_AddNetworkAdapter_Validation はバリデーション。
func TestClient_AddNetworkAdapter_Validation(t *testing.T) {
	client, _ := NewClient("http://localhost")
	if _, err := client.AddNetworkAdapter(context.Background(), "", NetworkAdapterOptions{ElementName: "x"}); err == nil {
		t.Error("expected error for empty vmName")
	}
	if _, err := client.AddNetworkAdapter(context.Background(), "vm", NetworkAdapterOptions{}); err == nil {
		t.Error("expected error for empty ElementName")
	}
	if _, err := client.AddNetworkAdapter(context.Background(), "vm", NetworkAdapterOptions{
		ElementName:      "x",
		StaticMacAddress: true,
	}); err == nil {
		t.Error("expected error for missing MAC when StaticMacAddress=true")
	}
}

// TestClient_RemoveNetworkAdapter は削除リクエストの組み立てを検証する。
func TestClient_RemoveNetworkAdapter(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_remove_resource_settings.xml")

	var bodies []string
	server := newSequenceServer(t, []string{respXML}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	jobRef, err := client.RemoveNetworkAdapter(context.Background(),
		`Microsoft:11111111-aaaa-bbbb-cccc-000000000001\NIC-001`)
	if err != nil {
		t.Fatalf("RemoveNetworkAdapter: %v", err)
	}
	if jobRef == "" {
		t.Error("jobRef should not be empty")
	}

	body := bodies[0]
	if !strings.Contains(body, "RemoveResourceSettings") {
		t.Errorf("body should call RemoveResourceSettings")
	}
	if !strings.Contains(body, "Msvm_SyntheticEthernetPortSettingData") {
		t.Errorf("body EPR should reference SyntheticEthernetPortSettingData")
	}
}

// TestClient_RemoveNetworkAdapter_Empty はバリデーション。
func TestClient_RemoveNetworkAdapter_Empty(t *testing.T) {
	client, _ := NewClient("http://localhost")
	if _, err := client.RemoveNetworkAdapter(context.Background(), ""); err == nil {
		t.Error("expected error for empty adapterInstanceID")
	}
}
