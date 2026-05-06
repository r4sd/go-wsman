package hyperv

import (
	"context"
	"strings"
	"testing"
)

// TestClient_ListExternalEthernetPorts は物理 NIC 一覧取得を検証する。
func TestClient_ListExternalEthernetPorts(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_externalethernetport.xml")
	pull := loadGolden(t, "pull_response_externalethernetport.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListExternalEthernetPorts(context.Background())
	if err != nil {
		t.Fatalf("ListExternalEthernetPorts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len: got %d, want 2", len(got))
	}
	if got[0].ElementName != "Realtek Gaming 2.5GbE" {
		t.Errorf("ElementName: %q", got[0].ElementName)
	}
	if got[0].IsBound {
		t.Errorf("IsBound[0]: want false")
	}
	if !got[1].IsBound {
		t.Errorf("IsBound[1]: want true")
	}
}

// TestClient_CreateSwitch_Private は Private Switch 作成リクエストを検証する。
//
// Private は ResourceSettings なし。リクエストには SystemSettings のみ。
func TestClient_CreateSwitch_Private(t *testing.T) {
	resp := loadGolden(t, "invoke_response_define_switch.xml")

	var bodies []string
	server := newSequenceServer(t, []string{resp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{
		Name: "PrivateSwitch",
		Type: SwitchTypePrivate,
	})
	if err != nil {
		t.Fatalf("CreateSwitch: %v", err)
	}
	if got.SwitchRef == "" {
		t.Errorf("SwitchRef should not be empty")
	}

	body := bodies[0]
	if !strings.Contains(body, "DefineSystem") {
		t.Errorf("body should call DefineSystem")
	}
	if !strings.Contains(body, "PrivateSwitch") {
		t.Errorf("body should contain switch name")
	}
	// Private は ResourceSettings 0 個
	if strings.Contains(body, "<p:ResourceSettings>") {
		t.Errorf("Private switch body should not contain ResourceSettings element")
	}
}

// TestClient_CreateSwitch_Internal は Internal Switch 作成を検証する。
//
// Internal は ResourceSettings に Internal Port 1 個。HostResource は持たない。
func TestClient_CreateSwitch_Internal(t *testing.T) {
	resp := loadGolden(t, "invoke_response_define_switch.xml")

	var bodies []string
	server := newSequenceServer(t, []string{resp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{
		Name: "InternalSwitch",
		Type: SwitchTypeInternal,
	})
	if err != nil {
		t.Fatalf("CreateSwitch: %v", err)
	}

	body := bodies[0]
	if strings.Count(body, "<p:ResourceSettings>") != 1 {
		t.Errorf("Internal switch body should contain exactly 1 ResourceSettings, body=%s", body)
	}
	if !strings.Contains(body, "Msvm_EthernetPortAllocationSettingData") {
		t.Errorf("body should contain Msvm_EthernetPortAllocationSettingData")
	}
	// Internal Port は HostResource を持たない
	if strings.Contains(body, "<p:HostResource>") {
		t.Errorf("Internal switch should not have HostResource (no physical NIC)")
	}
}

// TestClient_CreateSwitch_External は External Switch 作成を検証する。
//
// 想定リクエスト順 (3 件):
//
//	1-2: ListExternalEthernetPorts (enum + pull) — buildExternalAdapterBinding 内
//	3: DefineSystem invoke
//
// AllowManagementOS=true なら ResourceSettings は 2 個 (External binding + Internal port)。
func TestClient_CreateSwitch_External(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_externalethernetport.xml")
	pull := loadGolden(t, "pull_response_externalethernetport.xml")
	resp := loadGolden(t, "invoke_response_define_switch.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull, resp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{
		Name:              "ExternalSwitch",
		Type:              SwitchTypeExternal,
		ExternalAdapter:   "Realtek Gaming 2.5GbE",
		AllowManagementOS: true,
	})
	if err != nil {
		t.Fatalf("CreateSwitch: %v", err)
	}

	if len(bodies) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(bodies))
	}

	invokeBody := bodies[2]
	// AllowManagementOS=true → External binding + Internal Port = 2 個
	if strings.Count(invokeBody, "<p:ResourceSettings>") != 2 {
		t.Errorf("External switch with AllowManagementOS should have 2 ResourceSettings")
	}
	// External NIC の Name (Realtek の GUID) が HostResource に含まれる
	if !strings.Contains(invokeBody, "CCCCCCCC-3333-3333-3333-CCCCCCCCCCCC") {
		t.Errorf("invoke body should reference selected External NIC GUID")
	}
}

// TestClient_CreateSwitch_External_NotFound は ExternalAdapter が存在しないときのエラー。
func TestClient_CreateSwitch_External_NotFound(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_externalethernetport.xml")
	pull := loadGolden(t, "pull_response_externalethernetport.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{
		Name:            "ExternalSwitch",
		Type:            SwitchTypeExternal,
		ExternalAdapter: "NonExistent NIC",
	})
	if err == nil {
		t.Fatal("expected error for non-existent external adapter")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found: %v", err)
	}
}

// TestClient_CreateSwitch_Validation はバリデーション。
func TestClient_CreateSwitch_Validation(t *testing.T) {
	client, _ := NewClient("http://localhost")

	if _, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{Type: SwitchTypePrivate}); err == nil {
		t.Error("expected error for empty Name")
	}
	if _, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{Name: "x"}); err == nil {
		t.Error("expected error for empty Type")
	}
	if _, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{
		Name: "x", Type: SwitchTypeExternal,
	}); err == nil {
		t.Error("expected error for missing ExternalAdapter")
	}
}

// TestClient_DestroySwitch は削除リクエストの組み立てを検証する。
//
// 想定: GetVirtualEthernetSwitch (List → Filter) + DestroySystem invoke = 3 リクエスト。
func TestClient_DestroySwitch(t *testing.T) {
	swEnum := loadGolden(t, "enumerate_response_virtualethernetswitch.xml")
	swPull := loadGolden(t, "pull_response_virtualethernetswitch.xml")
	resp := loadGolden(t, "invoke_response_destroy_switch.xml")

	var bodies []string
	server := newSequenceServer(t, []string{swEnum, swPull, resp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	jobRef, err := client.DestroySwitch(context.Background(), "External")
	if err != nil {
		t.Fatalf("DestroySwitch: %v", err)
	}
	if jobRef == "" {
		t.Error("jobRef should not be empty")
	}

	invokeBody := bodies[2]
	if !strings.Contains(invokeBody, "DestroySystem") {
		t.Errorf("body should call DestroySystem")
	}
	// AffectedSystem に External の Name (GUID) が含まれること
	if !strings.Contains(invokeBody, "AAAAAAAA-1111-1111-1111-AAAAAAAAAAAA") {
		t.Errorf("body should reference target switch GUID")
	}
}

// TestClient_DestroySwitch_Empty
func TestClient_DestroySwitch_Empty(t *testing.T) {
	client, _ := NewClient("http://localhost")
	if _, err := client.DestroySwitch(context.Background(), ""); err == nil {
		t.Error("expected error for empty switchName")
	}
}
