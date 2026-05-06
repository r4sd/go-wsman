package hyperv

import (
	"context"
	"strings"
	"testing"
)

// TestClient_ListIDEControllers は VM の IDE Controller 一覧取得を検証する。
func TestClient_ListIDEControllers(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_idecontroller.xml")
	pull := loadGolden(t, "pull_response_idecontroller.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListIDEControllers(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("ListIDEControllers: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len: got %d, want 2", len(got))
	}
	if got[0].ResourceType != ResourceTypeIDEController {
		t.Errorf("ResourceType: %d", got[0].ResourceType)
	}
	if got[0].ResourceSubType != ResourceSubTypeIDEController {
		t.Errorf("ResourceSubType: %q", got[0].ResourceSubType)
	}

	// WQL に IDE Controller の SubType フィルタが含まれること
	if !strings.Contains(bodies[0], "Emulated IDE Controller") {
		t.Errorf("WQL should filter by IDE Controller subtype")
	}
}

// TestClient_ListAttachedStorage は VM にアタッチされたストレージ一覧を返す。
func TestClient_ListAttachedStorage(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_storageallocation.xml")
	pull := loadGolden(t, "pull_response_storageallocation.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListAttachedStorage(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("ListAttachedStorage: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	if got[0].HostResource != `C:\VMs\test.vhdx` {
		t.Errorf("HostResource: %q", got[0].HostResource)
	}
	if got[0].ResourceSubType != ResourceSubTypeVirtualHardDisk {
		t.Errorf("ResourceSubType: %q", got[0].ResourceSubType)
	}
}

// TestClient_AttachVHD は IDE Controller への VHD アタッチを検証する。
//
// 想定リクエスト順 (8 件):
//
//	1-2: ListIDEControllers (enumerate + pull)
//	3-5: AddResourceSettings (Drive)
//	6-8: AddResourceSettings (Storage)
//
// AddResourceSettings は内部で GetSystemSettingData (enum + pull) → invoke の 3 段階。
func TestClient_AttachVHD(t *testing.T) {
	ideEnum := loadGolden(t, "enumerate_response_idecontroller.xml")
	idePull := loadGolden(t, "pull_response_idecontroller.xml")
	sysEnum := loadGolden(t, "enumerate_response_systemsettingdata.xml")
	sysPull := loadGolden(t, "pull_response_systemsettingdata.xml")
	addResp := loadGolden(t, "invoke_response_add_resource_settings.xml")

	responses := []string{
		ideEnum, idePull, // ListIDEControllers
		sysEnum, sysPull, addResp, // Drive 追加
		sysEnum, sysPull, addResp, // Storage 追加
	}

	var bodies []string
	server := newSequenceServer(t, responses, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.AttachVHD(context.Background(),
		"11111111-aaaa-bbbb-cccc-000000000001",
		AttachVHDOptions{
			ControllerType:     ControllerTypeIDE,
			ControllerNumber:   0,
			ControllerLocation: 0,
			Path:               `C:\VMs\disk.vhdx`,
		})
	if err != nil {
		t.Fatalf("AttachVHD: %v", err)
	}
	if got.DriveRef == "" {
		t.Errorf("DriveRef should not be empty")
	}
	if got.StorageRef == "" {
		t.Errorf("StorageRef should not be empty")
	}

	if len(bodies) != 8 {
		t.Fatalf("expected 8 requests, got %d", len(bodies))
	}

	// 5 番目 (Drive 追加 invoke) に Synthetic Disk Drive が含まれる
	driveBody := bodies[4]
	if !strings.Contains(driveBody, ResourceSubTypeSyntheticDiskDrive) {
		t.Errorf("drive body should contain Synthetic Disk Drive subtype")
	}
	if !strings.Contains(driveBody, `<p:AddressOnParent>0</p:AddressOnParent>`) {
		t.Errorf("drive body should contain AddressOnParent=0")
	}

	// 8 番目 (Storage 追加 invoke) に VHD パスが含まれる
	storageBody := bodies[7]
	if !strings.Contains(storageBody, ResourceSubTypeVirtualHardDisk) {
		t.Errorf("storage body should contain Virtual Hard Disk subtype")
	}
	if !strings.Contains(storageBody, `C:\VMs\disk.vhdx`) {
		t.Errorf("storage body should contain VHD path")
	}
}

// TestClient_AttachDVD は ISO の DVD ドライブマウントを検証する。
//
// AttachVHD と同じ 8 リクエスト構成だが、ResourceSubType が DVD 系。
func TestClient_AttachDVD(t *testing.T) {
	ideEnum := loadGolden(t, "enumerate_response_idecontroller.xml")
	idePull := loadGolden(t, "pull_response_idecontroller.xml")
	sysEnum := loadGolden(t, "enumerate_response_systemsettingdata.xml")
	sysPull := loadGolden(t, "pull_response_systemsettingdata.xml")
	addResp := loadGolden(t, "invoke_response_add_resource_settings.xml")

	responses := []string{
		ideEnum, idePull,
		sysEnum, sysPull, addResp,
		sysEnum, sysPull, addResp,
	}

	var bodies []string
	server := newSequenceServer(t, responses, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.AttachDVD(context.Background(),
		"11111111-aaaa-bbbb-cccc-000000000001",
		AttachDVDOptions{
			ControllerType:     ControllerTypeIDE,
			ControllerNumber:   1,
			ControllerLocation: 0,
			Path:               `C:\ISOs\install.iso`,
		})
	if err != nil {
		t.Fatalf("AttachDVD: %v", err)
	}

	// Drive subtype は DVD
	if !strings.Contains(bodies[4], ResourceSubTypeSyntheticDVDDrive) {
		t.Errorf("drive body should contain Synthetic DVD Drive subtype")
	}
	// Storage subtype は CD/DVD
	if !strings.Contains(bodies[7], ResourceSubTypeVirtualCDDVDDisk) {
		t.Errorf("storage body should contain Virtual CD/DVD Disk subtype")
	}
	if !strings.Contains(bodies[7], `C:\ISOs\install.iso`) {
		t.Errorf("storage body should contain ISO path")
	}
}

// TestClient_AttachVHD_Validation はバリデーション。
func TestClient_AttachVHD_Validation(t *testing.T) {
	client, _ := NewClient("http://localhost")

	if _, err := client.AttachVHD(context.Background(), "", AttachVHDOptions{
		ControllerType: ControllerTypeIDE, Path: "x",
	}); err == nil {
		t.Error("expected error for empty vmName")
	}
	if _, err := client.AttachVHD(context.Background(), "vm", AttachVHDOptions{
		ControllerType: ControllerTypeIDE,
	}); err == nil {
		t.Error("expected error for empty Path")
	}
	if _, err := client.AttachVHD(context.Background(), "vm", AttachVHDOptions{
		ControllerType: ControllerTypeSCSI, Path: "x",
	}); err == nil {
		t.Error("expected error for SCSI (not supported in Phase 4)")
	}
}

// TestClient_DetachStorage は Drive の InstanceID で削除リクエストを組み立てるテスト。
func TestClient_DetachStorage(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_remove_resource_settings.xml")

	var bodies []string
	server := newSequenceServer(t, []string{respXML}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	jobRef, err := client.DetachStorage(context.Background(),
		`Microsoft:11111111-aaaa-bbbb-cccc-000000000001\DRIVE-001`)
	if err != nil {
		t.Fatalf("DetachStorage: %v", err)
	}
	if jobRef == "" {
		t.Error("jobRef should not be empty")
	}

	body := bodies[0]
	if !strings.Contains(body, "RemoveResourceSettings") {
		t.Errorf("body should call RemoveResourceSettings")
	}
	if !strings.Contains(body, "Msvm_ResourceAllocationSettingData") {
		t.Errorf("body EPR should reference ResourceAllocationSettingData")
	}
}

// TestClient_DetachStorage_Empty はバリデーション。
func TestClient_DetachStorage_Empty(t *testing.T) {
	client, _ := NewClient("http://localhost")
	if _, err := client.DetachStorage(context.Background(), ""); err == nil {
		t.Error("expected error for empty driveInstanceID")
	}
}
