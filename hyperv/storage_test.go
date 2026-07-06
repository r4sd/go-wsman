package hyperv

import (
	"context"
	"strings"
	"testing"
)

// TestClient_ListIDEControllers は VM の IDE Controller 一覧取得を検証する。
//
// Hyper-V は WQL フィルタ列挙を拒否する (#80) ため、無フィルタ列挙 + Go 側フィルタで
// 「対象 VM かつ ResourceSubType=Emulated IDE Controller」だけを返す。mixed golden は
// 対象 VM の IDE / 対象 VM の SCSI(別 SubType) / 別 VM の IDE を含めて、IDE 1 件だけを
// 選べることを検証する。
func TestClient_ListIDEControllers(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_idecontroller.xml")
	pull := loadGolden(t, "pull_response_idecontroller_mixed.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListIDEControllers(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("ListIDEControllers: %v", err)
	}
	// SCSI(別 SubType) と別 VM の IDE を除外し、対象 VM の IDE Controller 1 件だけ返ること。
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1 (SCSI / 別 VM の IDE を含めてはいけない)", len(got))
	}
	if got[0].ResourceType != ResourceTypeIDEController {
		t.Errorf("ResourceType: %d", got[0].ResourceType)
	}
	if got[0].ResourceSubType != ResourceSubTypeIDEController {
		t.Errorf("ResourceSubType: %q", got[0].ResourceSubType)
	}
	if got[0].InstanceID != `Microsoft:11111111-aaaa-bbbb-cccc-000000000001\IDE-CTRL-0` {
		t.Errorf("InstanceID: got %q (対象 VM の IDE であるべき)", got[0].InstanceID)
	}

	// Hyper-V は WQL フィルタ列挙を拒否するため、Enumerate は無フィルタで送ること (#80)。
	if strings.Contains(bodies[0], "Filter") || strings.Contains(bodies[0], "SELECT") {
		t.Errorf("enumerate should be unfiltered (no WQL Filter); body: %s", bodies[0])
	}
}

// TestClient_ListSCSIControllers は VM の SCSI Controller 一覧取得を検証する。
//
// ListIDEControllers と同じ無フィルタ列挙 + Go 側フィルタ方式で、ResourceSubType が
// Synthetic SCSI Controller のものだけを選ぶ。mixed golden は対象 VM の IDE / SCSI /
// 別 VM の IDE を含むので、対象 VM の SCSI 1 件だけを選べることを検証する。
func TestClient_ListSCSIControllers(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_idecontroller.xml")
	pull := loadGolden(t, "pull_response_idecontroller_mixed.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListSCSIControllers(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("ListSCSIControllers: %v", err)
	}
	// IDE(別 SubType) と別 VM を除外し、対象 VM の SCSI Controller 1 件だけ返ること。
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1 (IDE / 別 VM を含めてはいけない)", len(got))
	}
	if got[0].ResourceType != ResourceTypeParallelSCSI {
		t.Errorf("ResourceType: %d", got[0].ResourceType)
	}
	if got[0].ResourceSubType != ResourceSubTypeSCSIController {
		t.Errorf("ResourceSubType: %q", got[0].ResourceSubType)
	}
	if got[0].InstanceID != `Microsoft:11111111-aaaa-bbbb-cccc-000000000001\SCSI-CTRL-0` {
		t.Errorf("InstanceID: got %q (対象 VM の SCSI であるべき)", got[0].InstanceID)
	}

	// Hyper-V は WQL フィルタ列挙を拒否するため、Enumerate は無フィルタで送ること (#80)。
	if strings.Contains(bodies[0], "Filter") || strings.Contains(bodies[0], "SELECT") {
		t.Errorf("enumerate should be unfiltered (no WQL Filter); body: %s", bodies[0])
	}
}

// TestClient_AddScsiController は VM への SCSI Controller 追加を検証する。
//
// go-wsman の DefineSystem はシェル VM を作り Gen2 でも SCSI Controller を持たないため
// (#88)、SCSI ブートには本メソッドで明示追加する。AddResourceSettings は内部で
// GetSystemSettingData (enum + pull) → invoke の 3 リクエスト。
func TestClient_AddScsiController(t *testing.T) {
	sysEnum := loadGolden(t, "enumerate_response_systemsettingdata.xml")
	sysPull := loadGolden(t, "pull_response_systemsettingdata.xml")
	addResp := loadGolden(t, "invoke_response_add_resource_settings.xml")

	var bodies []string
	server := newSequenceServer(t, []string{sysEnum, sysPull, addResp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.AddScsiController(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("AddScsiController: %v", err)
	}
	if got.ControllerRef == "" {
		t.Errorf("ControllerRef should not be empty")
	}
	if len(bodies) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(bodies))
	}
	// invoke (3 番目) に Synthetic SCSI Controller subtype と ResourceType=6 が入ること。
	invokeBody := bodies[2]
	if !strings.Contains(invokeBody, ResourceSubTypeSCSIController) {
		t.Errorf("invoke body should contain Synthetic SCSI Controller subtype")
	}
	if !strings.Contains(invokeBody, `<PROPERTY NAME="ResourceType" TYPE="uint16"><VALUE>6</VALUE></PROPERTY>`) {
		t.Errorf("invoke body should contain ResourceType=6 (Parallel SCSI HBA)")
	}
}

// TestClient_AddScsiController_Validation はバリデーション。
func TestClient_AddScsiController_Validation(t *testing.T) {
	client, _ := NewClient("http://localhost")
	if _, err := client.AddScsiController(context.Background(), ""); err == nil {
		t.Error("expected error for empty vmName")
	}
}

// TestClient_ListDiskDrives は VM のディスクドライブ (Msvm_ResourceAllocationSettingData,
// ResourceType=17) 一覧取得を検証する。
//
// GetVmHardDiskDrives の逆引き (storage→drive→controller) に使う。ドライブは Parent に
// 親 Controller の EPR、AddressOnParent に Controller 内 location を持つ。mixed golden は
// 対象 VM の Disk Drive / 対象 VM の SCSI Controller(別 SubType) / 別 VM の Disk Drive を
// 含み、対象 VM の Disk Drive 1 件だけを選べることを検証する。
// TestClient_ListDvdDrives は対象 VM の Synthetic DVD Drive のみを返し、Disk Drive・別 VM を
// 除外することを検証する (逆引きに必要な Parent/AddressOnParent も取れること)。
func TestClient_ListDvdDrives(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_idecontroller.xml")
	pull := loadGolden(t, "pull_response_dvddrive_mixed.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListDvdDrives(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("ListDvdDrives: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1 (Disk Drive / 別 VM を含めてはいけない)", len(got))
	}
	if got[0].ResourceType != ResourceTypeDVDDrive {
		t.Errorf("ResourceType: got %d, want %d", got[0].ResourceType, ResourceTypeDVDDrive)
	}
	if got[0].ResourceSubType != ResourceSubTypeSyntheticDVDDrive {
		t.Errorf("ResourceSubType: %q", got[0].ResourceSubType)
	}
	if got[0].AddressOnParent != "1" {
		t.Errorf("AddressOnParent: got %q, want 1", got[0].AddressOnParent)
	}
	if !strings.Contains(got[0].Parent, `SCSI-CTRL-0`) {
		t.Errorf("Parent should reference the SCSI controller; got %q", got[0].Parent)
	}
	if strings.Contains(bodies[0], "Filter") || strings.Contains(bodies[0], "SELECT") {
		t.Errorf("enumerate should be unfiltered (no WQL Filter); body: %s", bodies[0])
	}
}

func TestClient_ListDiskDrives(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_idecontroller.xml")
	pull := loadGolden(t, "pull_response_diskdrive_mixed.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListDiskDrives(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("ListDiskDrives: %v", err)
	}
	// Controller(別 SubType) と別 VM の Disk Drive を除外し、対象 VM の Disk Drive 1 件だけ。
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1 (Controller / 別 VM を含めてはいけない)", len(got))
	}
	if got[0].ResourceType != ResourceTypeDiskDrive {
		t.Errorf("ResourceType: %d", got[0].ResourceType)
	}
	if got[0].ResourceSubType != ResourceSubTypeSyntheticDiskDrive {
		t.Errorf("ResourceSubType: %q", got[0].ResourceSubType)
	}
	// 逆引きに必要な Parent(親 Controller) と AddressOnParent(location) が取れること。
	if got[0].AddressOnParent != "0" {
		t.Errorf("AddressOnParent: got %q, want 0", got[0].AddressOnParent)
	}
	if !strings.Contains(got[0].Parent, `SCSI-CTRL-0`) {
		t.Errorf("Parent should reference the SCSI controller; got %q", got[0].Parent)
	}

	// Hyper-V は WQL フィルタ列挙を拒否するため、Enumerate は無フィルタで送ること (#80)。
	if strings.Contains(bodies[0], "Filter") || strings.Contains(bodies[0], "SELECT") {
		t.Errorf("enumerate should be unfiltered (no WQL Filter); body: %s", bodies[0])
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
	if got[0].HostResource != `D:\VMs\test.vhdx` {
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
	jobResp := loadGolden(t, "get_response_concretejob_completed.xml")

	responses := []string{
		ideEnum, idePull, // ListIDEControllers
		sysEnum, sysPull, addResp, jobResp, // Drive 追加 + Job 完了待ち
		sysEnum, sysPull, addResp, jobResp, // Storage 追加 + Job 完了待ち
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
			Path:               `D:\VMs\disk.vhdx`,
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

	if len(bodies) != 10 {
		t.Fatalf("expected 10 requests, got %d", len(bodies))
	}

	// 5 番目 (Drive 追加 invoke) に Synthetic Disk Drive が含まれる
	driveBody := bodies[4]
	if !strings.Contains(driveBody, ResourceSubTypeSyntheticDiskDrive) {
		t.Errorf("drive body should contain Synthetic Disk Drive subtype")
	}
	// embedded instance は CIM-XML <PROPERTY> 形式で CDATA 内に入る (#81)。
	if !strings.Contains(driveBody, `<PROPERTY NAME="AddressOnParent" TYPE="string"><VALUE>0</VALUE></PROPERTY>`) {
		t.Errorf("drive body should contain AddressOnParent=0")
	}

	// Storage 追加 invoke (bodies[8]) に VHD パスが含まれる
	storageBody := bodies[8]
	if !strings.Contains(storageBody, ResourceSubTypeVirtualHardDisk) {
		t.Errorf("storage body should contain Virtual Hard Disk subtype")
	}
	if !strings.Contains(storageBody, `D:\VMs\disk.vhdx`) {
		t.Errorf("storage body should contain VHD path")
	}
}

// TestClient_AttachVHD_SCSI は SCSI Controller への VHD アタッチを検証する。
//
// Gen2 VM は IDE を持たずブートディスクは SCSI 必須。AttachVHD と同じ 8 リクエスト構成だが、
// ターゲット列挙が ListSCSIControllers になり、AddressOnParent は SCSI レンジ (0-63) を取る。
func TestClient_AttachVHD_SCSI(t *testing.T) {
	scsiEnum := loadGolden(t, "enumerate_response_idecontroller.xml")
	scsiPull := loadGolden(t, "pull_response_idecontroller_mixed.xml") // 対象 VM の SCSI 1 件を含む
	sysEnum := loadGolden(t, "enumerate_response_systemsettingdata.xml")
	sysPull := loadGolden(t, "pull_response_systemsettingdata.xml")
	addResp := loadGolden(t, "invoke_response_add_resource_settings.xml")
	jobResp := loadGolden(t, "get_response_concretejob_completed.xml")

	responses := []string{
		scsiEnum, scsiPull, // ListSCSIControllers
		sysEnum, sysPull, addResp, jobResp, // Drive 追加 + Job 完了待ち
		sysEnum, sysPull, addResp, jobResp, // Storage 追加 + Job 完了待ち
	}

	var bodies []string
	server := newSequenceServer(t, responses, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.AttachVHD(context.Background(),
		"11111111-aaaa-bbbb-cccc-000000000001",
		AttachVHDOptions{
			ControllerType:     ControllerTypeSCSI,
			ControllerNumber:   0,
			ControllerLocation: 5, // SCSI は 0-63
			Path:               `D:\VMs\talos.vhdx`,
		})
	if err != nil {
		t.Fatalf("AttachVHD(SCSI): %v", err)
	}
	if got.DriveRef == "" || got.StorageRef == "" {
		t.Errorf("DriveRef/StorageRef should not be empty")
	}
	if len(bodies) != 10 {
		t.Fatalf("expected 10 requests, got %d", len(bodies))
	}

	// Drive 追加 invoke に SCSI レンジの AddressOnParent が入ること。
	driveBody := bodies[4]
	if !strings.Contains(driveBody, ResourceSubTypeSyntheticDiskDrive) {
		t.Errorf("drive body should contain Synthetic Disk Drive subtype")
	}
	if !strings.Contains(driveBody, `<PROPERTY NAME="AddressOnParent" TYPE="string"><VALUE>5</VALUE></PROPERTY>`) {
		t.Errorf("drive body should contain AddressOnParent=5")
	}
	// Drive の Parent が SCSI Controller の InstanceID を指すこと。
	if !strings.Contains(driveBody, `SCSI-CTRL-0`) {
		t.Errorf("drive body Parent should reference the SCSI controller")
	}

	storageBody := bodies[8]
	if !strings.Contains(storageBody, `D:\VMs\talos.vhdx`) {
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
	jobResp := loadGolden(t, "get_response_concretejob_completed.xml")

	responses := []string{
		ideEnum, idePull,
		sysEnum, sysPull, addResp, jobResp,
		sysEnum, sysPull, addResp, jobResp,
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
			Path:               `D:\ISOs\install.iso`,
		})
	if err != nil {
		t.Fatalf("AttachDVD: %v", err)
	}

	// Drive subtype は DVD
	if !strings.Contains(bodies[4], ResourceSubTypeSyntheticDVDDrive) {
		t.Errorf("drive body should contain Synthetic DVD Drive subtype")
	}
	// Storage subtype は CD/DVD (bodies[8])
	if !strings.Contains(bodies[8], ResourceSubTypeVirtualCDDVDDisk) {
		t.Errorf("storage body should contain Virtual CD/DVD Disk subtype")
	}
	if !strings.Contains(bodies[8], `D:\ISOs\install.iso`) {
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
	// 未知の ControllerType は拒否。
	if _, err := client.AttachVHD(context.Background(), "vm", AttachVHDOptions{
		ControllerType: ControllerType("USB"), Path: "x",
	}); err == nil {
		t.Error("expected error for unknown controller type")
	}
	// SCSI の AddressOnParent は 0-63。範囲外は拒否。
	if _, err := client.AttachVHD(context.Background(), "vm", AttachVHDOptions{
		ControllerType: ControllerTypeSCSI, ControllerLocation: 64, Path: "x",
	}); err == nil {
		t.Error("expected error for SCSI AddressOnParent out of range (64)")
	}
	// IDE の location は 0-1。範囲外は拒否。
	if _, err := client.AttachVHD(context.Background(), "vm", AttachVHDOptions{
		ControllerType: ControllerTypeIDE, ControllerLocation: 2, Path: "x",
	}); err == nil {
		t.Error("expected error for IDE location out of range (2)")
	}
}

// TestClient_DetachStorage は「Storage (SASD) を先に削除 → Drive (RASD) を削除」の
// 2段・別リクエストを、それぞれ正しい ResourceURI の EPR で組み立てることを検証する。
// 子 SASD を残したまま Drive を削除すると VMMS が 0x80041001 で拒否するため、順序が重要 (#97)。
func TestClient_DetachStorage(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_remove_resource_settings.xml")
	jobResp := loadGolden(t, "get_response_concretejob_completed.xml")

	var bodies []string
	// SASD 削除 (remove + job wait) → RASD 削除 (remove + job wait) の 4 レスポンス。
	server := newSequenceServer(t, []string{respXML, jobResp, respXML, jobResp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	jobRef, err := client.DetachStorage(context.Background(),
		`Microsoft:11111111-aaaa-bbbb-cccc-000000000001\DRIVE-001`,
		`Microsoft:11111111-aaaa-bbbb-cccc-000000000001\STORAGE-001`)
	if err != nil {
		t.Fatalf("DetachStorage: %v", err)
	}
	if jobRef == "" {
		t.Error("jobRef should not be empty")
	}

	// 1発目の RemoveResourceSettings は Storage (SASD) を対象にする。
	storageBody := bodies[0]
	if !strings.Contains(storageBody, "RemoveResourceSettings") {
		t.Errorf("1st body should call RemoveResourceSettings")
	}
	if !strings.Contains(storageBody, "Msvm_StorageAllocationSettingData") {
		t.Errorf("1st body EPR should reference StorageAllocationSettingData (SASD first), got: %s", storageBody)
	}
	if !strings.Contains(storageBody, `STORAGE-001`) {
		t.Errorf("1st body should target the storage InstanceID")
	}

	// 3発目の RemoveResourceSettings は Drive (RASD) を対象にする。
	driveBody := bodies[2]
	if !strings.Contains(driveBody, "Msvm_ResourceAllocationSettingData") {
		t.Errorf("3rd body EPR should reference ResourceAllocationSettingData (RASD second), got: %s", driveBody)
	}
	if !strings.Contains(driveBody, `DRIVE-001`) {
		t.Errorf("3rd body should target the drive InstanceID")
	}
}

// TestClient_DetachStorage_DriveOnly は storageInstanceID 空文字のとき Drive 単独削除に
// フォールバックすること (attach rollback の空 Drive 掃除用途) を検証する。
func TestClient_DetachStorage_DriveOnly(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_remove_resource_settings.xml")
	jobResp := loadGolden(t, "get_response_concretejob_completed.xml")

	var bodies []string
	// Storage 削除をスキップするので remove + job wait の 2 レスポンスのみ。
	server := newSequenceServer(t, []string{respXML, jobResp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	if _, err := client.DetachStorage(context.Background(),
		`Microsoft:11111111-aaaa-bbbb-cccc-000000000001\DRIVE-001`, ""); err != nil {
		t.Fatalf("DetachStorage: %v", err)
	}
	if !strings.Contains(bodies[0], "Msvm_ResourceAllocationSettingData") {
		t.Errorf("drive-only detach should target ResourceAllocationSettingData")
	}
}

// TestClient_DetachStorage_Empty はバリデーション。
func TestClient_DetachStorage_Empty(t *testing.T) {
	client, _ := NewClient("http://localhost")
	if _, err := client.DetachStorage(context.Background(), "", ""); err == nil {
		t.Error("expected error for empty driveInstanceID")
	}
}
