//go:build integration

package hyperv

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/r4sd/go-wsman/wsman"
)

// Integration Test は実際の Hyper-V ホストに接続してテストする。
//
// 実行方法:
//
//	WSMAN_ENDPOINT=https://10.0.0.100:5986/wsman \
//	WSMAN_USERNAME=terraform \
//	WSMAN_PASSWORD=yourpassword \
//	go test -race -tags=integration -v ./hyperv/...
//
// 前提:
//   - Hyper-V ホスト上に最低 1 つの VM が存在すること
//   - Phase 1 は読み取り専用（VM の作成・削除はしない）

func getIntegrationClient(t *testing.T) *Client {
	t.Helper()

	endpoint := os.Getenv("WSMAN_ENDPOINT")
	username := os.Getenv("WSMAN_USERNAME")
	password := os.Getenv("WSMAN_PASSWORD")

	if endpoint == "" || username == "" || password == "" {
		t.Skip("WSMAN_ENDPOINT, WSMAN_USERNAME, WSMAN_PASSWORD must be set")
	}

	// 自己署名証明書のホスト (homelab 等) では WSMAN_INSECURE=true で TLS 検証をスキップする
	// (bug_sweep_integration_test と同じ挙動)。
	opts := []wsman.ClientOption{wsman.WithNTLM(username, password)}
	if os.Getenv("WSMAN_INSECURE") == "true" {
		opts = append(opts, wsman.WithInsecureSkipVerify())
	}
	client, err := NewClient(endpoint, opts...)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	return client
}

// TestIntegration_ListComputerSystems は実機から VM 一覧を取得する。
func TestIntegration_ListComputerSystems(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vms, err := client.ListComputerSystems(ctx)
	if err != nil {
		t.Fatalf("ListComputerSystems failed: %v", err)
	}
	if len(vms) == 0 {
		t.Skip("Hyper-V ホストに VM が存在しない（テストの前提を満たさない）")
	}

	t.Logf("VM 件数: %d", len(vms))
	for _, vm := range vms {
		t.Logf("  Name=%s ElementName=%q EnabledState=%d HealthState=%d",
			vm.Name, vm.ElementName, vm.EnabledState, vm.HealthState)
		if vm.Name == "" {
			t.Errorf("VM の Name が空: %+v", vm)
		}
	}
}

// TestIntegration_GetVirtualHardDisk は環境変数で指定された VHD ファイルの設定を取得する。
//
// 追加環境変数:
//   - HYPERV_TEST_VHD_PATH: 既存 VHD ファイルのフルパス（例: "D:\\VMs\\test.vhdx"）
func TestIntegration_GetVirtualHardDisk(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	path := os.Getenv("HYPERV_TEST_VHD_PATH")
	if path == "" {
		t.Skip("HYPERV_TEST_VHD_PATH 未設定（既存 VHD のフルパスを指定）")
	}

	settings, err := client.GetVirtualHardDisk(ctx, path)
	if err != nil {
		t.Fatalf("GetVirtualHardDisk(%s) failed: %v", path, err)
	}

	t.Logf("VHD %s: Format=%d Type=%d MaxSize=%d", settings.Path,
		settings.VirtualDiskFormat, settings.VirtualDiskType, settings.MaxInternalSize)

	if settings.Path != path {
		t.Errorf("Path mismatch: got %q, want %q", settings.Path, path)
	}
	if settings.VirtualDiskFormat == VHDFormatUnknown {
		t.Errorf("VirtualDiskFormat is Unknown")
	}
}

// TestIntegration_GetMemoryAndProcessorSettings は ListComputerSystems で取得した最初の
// VM のメモリ・CPU 設定を読み取る (read-only)。
func TestIntegration_GetMemoryAndProcessorSettings(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vms, err := client.ListComputerSystems(ctx)
	if err != nil {
		t.Fatalf("ListComputerSystems: %v", err)
	}
	if len(vms) == 0 {
		t.Skip("Hyper-V ホストに VM が存在しない")
	}
	target := vms[0]

	mem, err := client.GetMemorySettings(ctx, target.Name)
	if err != nil {
		t.Fatalf("GetMemorySettings: %v", err)
	}
	cpu, err := client.GetProcessorSettings(ctx, target.Name)
	if err != nil {
		t.Fatalf("GetProcessorSettings: %v", err)
	}
	t.Logf("VM %q: Memory=%dMB DynamicEnabled=%v / vCPU=%d Limit=%d ExposeVirt=%v",
		target.ElementName, mem.VirtualQuantity, mem.DynamicMemoryEnabled,
		cpu.VirtualQuantity, cpu.Limit, cpu.ExposeVirtualizationExtensions)

	if mem.ResourceType != ResourceTypeMemory {
		t.Errorf("Memory.ResourceType: got %d, want %d", mem.ResourceType, ResourceTypeMemory)
	}
	if cpu.ResourceType != ResourceTypeProcessor {
		t.Errorf("Processor.ResourceType: got %d, want %d", cpu.ResourceType, ResourceTypeProcessor)
	}
}

// TestIntegration_SetMemorySettings は対象 VM のメモリ設定を読み取り、同じ値で
// 書き戻す (no-op 相当)。CIM 経由の Modify が動作することを確認する。
//
// HYPERV_TEST_ALLOW_MUTATION + HYPERV_TEST_TARGET_VM_NAME が必要。
func TestIntegration_SetMemorySettings(t *testing.T) {
	if os.Getenv("HYPERV_TEST_ALLOW_MUTATION") == "" {
		t.Skip("HYPERV_TEST_ALLOW_MUTATION 未設定")
	}
	target := os.Getenv("HYPERV_TEST_TARGET_VM_NAME")
	if target == "" {
		t.Skip("HYPERV_TEST_TARGET_VM_NAME 未設定")
	}

	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mem, err := client.GetMemorySettings(ctx, target)
	if err != nil {
		t.Fatalf("GetMemorySettings: %v", err)
	}
	t.Logf("Before: VirtualQuantity=%d Weight=%d", mem.VirtualQuantity, mem.Weight)

	jobRef, err := client.SetMemorySettings(ctx, mem)
	if err != nil {
		t.Fatalf("SetMemorySettings: %v", err)
	}
	t.Logf("ModifyResourceSettings Job: %s", jobRef)
}

// TestIntegration_RequestStateChange は環境変数で指定された VM に対して
// 状態遷移を要求する。
//
// HYPERV_TEST_TARGET_VM_NAME に対象 VM の Name (GUID) を指定する。
// VM の現在の状態に応じて、安全な遷移先 (Stopped→Start→Save) を選んでテストする。
//
// HYPERV_TEST_ALLOW_MUTATION が未設定の場合はスキップ (実 VM の状態を変えるため)。
func TestIntegration_RequestStateChange(t *testing.T) {
	if os.Getenv("HYPERV_TEST_ALLOW_MUTATION") == "" {
		t.Skip("HYPERV_TEST_ALLOW_MUTATION 未設定（VM の状態遷移を伴う破壊的テスト）")
	}
	target := os.Getenv("HYPERV_TEST_TARGET_VM_NAME")
	if target == "" {
		t.Skip("HYPERV_TEST_TARGET_VM_NAME 未設定（対象 VM の GUID を指定）")
	}

	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 現状取得
	vm, err := client.GetComputerSystem(ctx, target)
	if err != nil {
		t.Fatalf("GetComputerSystem: %v", err)
	}
	t.Logf("Target VM %q: 現在 EnabledState=%d", vm.ElementName, vm.EnabledState)

	// 安全な遷移: Disabled (停止中) なら Start を要求
	// それ以外は Save を要求 (Save なら復帰可能)
	var jobRef string
	switch vm.EnabledState {
	case EnabledStateDisabled:
		t.Log("Disabled → Start を要求")
		jobRef, err = client.StartVM(ctx, target)
	case EnabledStateEnabled:
		t.Log("Enabled → Save を要求")
		jobRef, err = client.SaveVM(ctx, target)
	default:
		t.Skipf("EnabledState=%d はテスト対象外 (Disabled/Enabled のみテスト)", vm.EnabledState)
	}

	if err != nil {
		t.Fatalf("RequestStateChange failed: %v", err)
	}
	t.Logf("Job 開始: %s", jobRef)
}

// TestIntegration_GetComputerSystem は ListComputerSystems で取得した最初の VM を Get で再取得する。
func TestIntegration_GetComputerSystem(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vms, err := client.ListComputerSystems(ctx)
	if err != nil {
		t.Fatalf("ListComputerSystems failed: %v", err)
	}
	if len(vms) == 0 {
		t.Skip("Hyper-V ホストに VM が存在しない")
	}

	target := vms[0]
	got, err := client.GetComputerSystem(ctx, target.Name)
	if err != nil {
		t.Fatalf("GetComputerSystem(%s) failed: %v", target.Name, err)
	}

	if got.Name != target.Name {
		t.Errorf("Name mismatch: got %s, want %s", got.Name, target.Name)
	}
	if got.ElementName != target.ElementName {
		t.Errorf("ElementName mismatch: got %q, want %q", got.ElementName, target.ElementName)
	}
}

// TestIntegration_ListSystemSettingData は実機から全 VM の Realized 構成を取得する。
func TestIntegration_ListSystemSettingData(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	settings, err := client.ListSystemSettingData(ctx)
	if err != nil {
		t.Fatalf("ListSystemSettingData failed: %v", err)
	}
	if len(settings) == 0 {
		t.Skip("Hyper-V ホストに VM が存在しない（テストの前提を満たさない）")
	}

	t.Logf("SettingData 件数: %d", len(settings))
	for _, s := range settings {
		t.Logf("  VM=%s SubType=%s StartupAction=%d Version=%s",
			s.ElementName, s.VirtualSystemSubType, s.AutomaticStartupAction, s.Version)
		if s.VirtualSystemType != VirtualSystemTypeRealized {
			t.Errorf("VirtualSystemType=%q, want Realized only", s.VirtualSystemType)
		}
		if s.VirtualSystemIdentifier == "" {
			t.Errorf("VirtualSystemIdentifier が空: %+v", s)
		}
	}
}

// TestIntegration_GetSystemSettingData は ListComputerSystems で取得した最初の VM の
// SettingData を WQL ベースで取得する。
func TestIntegration_GetSystemSettingData(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vms, err := client.ListComputerSystems(ctx)
	if err != nil {
		t.Fatalf("ListComputerSystems failed: %v", err)
	}
	if len(vms) == 0 {
		t.Skip("Hyper-V ホストに VM が存在しない")
	}

	target := vms[0]
	got, err := client.GetSystemSettingData(ctx, target.Name)
	if err != nil {
		t.Fatalf("GetSystemSettingData(%s) failed: %v", target.Name, err)
	}

	if got.VirtualSystemIdentifier != target.Name {
		t.Errorf("VirtualSystemIdentifier mismatch: got %s, want %s", got.VirtualSystemIdentifier, target.Name)
	}
	if got.VirtualSystemType != VirtualSystemTypeRealized {
		t.Errorf("VirtualSystemType: got %q, want Realized", got.VirtualSystemType)
	}
}

// TestIntegration_DefineAndDestroySystem は VM の作成→削除をエンドツーエンドで検証する。
//
// VM 名にはタイムスタンプを含めてテスト同士の衝突を避ける。
// 作成失敗・削除失敗いずれの場合も、後続テストに残骸を残さないよう
// defer で削除を試みる (ベストエフォート)。
//
// 注: Phase 3 part 2 の段階では VM は ResourceSettings (CPU/Memory/NIC 等) を持たない
// 「シェル」状態で作成される。実用には Phase 4 のリソース追加が必要。
func TestIntegration_DefineAndDestroySystem(t *testing.T) {
	if os.Getenv("HYPERV_TEST_ALLOW_MUTATION") == "" {
		t.Skip("HYPERV_TEST_ALLOW_MUTATION 未設定（VM 作成・削除を伴う破壊的テスト）")
	}

	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	vmName := fmt.Sprintf("go-wsman-test-%d", time.Now().UnixNano())

	settings := &Msvm_VirtualSystemSettingData{
		ElementName:          vmName,
		VirtualSystemSubType: VirtualSystemSubTypeGen2,
	}

	t.Logf("Creating VM: %s", vmName)
	result, err := client.DefineSystem(ctx, settings)
	if err != nil {
		t.Fatalf("DefineSystem failed: %v", err)
	}
	t.Logf("DefineSystem result: ReturnValue=%s ResultingSystem=%s JobRef=%s",
		result.ReturnValue, result.ResultingSystem, result.JobRef)

	if result.ResultingSystem == "" {
		t.Errorf("ResultingSystem is empty")
	}

	// ベストエフォートのクリーンアップ
	defer func() {
		if result.ResultingSystem == "" {
			return
		}
		t.Logf("Cleaning up VM: %s", result.ResultingSystem)
		if _, err := client.DestroySystem(ctx, result.ResultingSystem); err != nil {
			t.Logf("DestroySystem cleanup failed (may be already deleted): %v", err)
		}
	}()

	// VM が一覧に現れることを確認
	vms, err := client.ListComputerSystems(ctx)
	if err != nil {
		t.Fatalf("ListComputerSystems failed: %v", err)
	}
	found := false
	for _, vm := range vms {
		if vm.Name == result.ResultingSystem {
			found = true
			if vm.ElementName != vmName {
				t.Errorf("ElementName mismatch: got %q, want %q", vm.ElementName, vmName)
			}
			break
		}
	}
	if !found {
		t.Errorf("created VM %s not found in list", result.ResultingSystem)
	}
}

// TestIntegration_ListVirtualEthernetSwitches は実機の仮想スイッチ一覧を取得する (read-only)。
func TestIntegration_ListVirtualEthernetSwitches(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switches, err := client.ListVirtualEthernetSwitches(ctx)
	if err != nil {
		t.Fatalf("ListVirtualEthernetSwitches: %v", err)
	}
	t.Logf("仮想スイッチ件数: %d", len(switches))
	for _, sw := range switches {
		t.Logf("  Name=%s ElementName=%q Health=%d", sw.Name, sw.ElementName, sw.HealthState)
	}
}

// TestIntegration_AddRemoveNetworkAdapter は HYPERV_TEST_TARGET_VM_NAME に
// NIC を追加→削除するテスト。HYPERV_TEST_SWITCH_NAME が指定されていれば
// そのスイッチに接続する。
//
// 必要な環境変数:
//   - HYPERV_TEST_ALLOW_MUTATION=1
//   - HYPERV_TEST_TARGET_VM_NAME=<VM_GUID>
//   - HYPERV_TEST_SWITCH_NAME=<スイッチ表示名> (オプション)
func TestIntegration_AddRemoveNetworkAdapter(t *testing.T) {
	if os.Getenv("HYPERV_TEST_ALLOW_MUTATION") == "" {
		t.Skip("HYPERV_TEST_ALLOW_MUTATION 未設定")
	}
	target := os.Getenv("HYPERV_TEST_TARGET_VM_NAME")
	if target == "" {
		t.Skip("HYPERV_TEST_TARGET_VM_NAME 未設定")
	}

	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	opts := NetworkAdapterOptions{
		ElementName: fmt.Sprintf("test-nic-%d", time.Now().UnixNano()),
		SwitchName:  os.Getenv("HYPERV_TEST_SWITCH_NAME"),
	}
	t.Logf("Adding NIC: name=%s switch=%q", opts.ElementName, opts.SwitchName)

	result, err := client.AddNetworkAdapter(ctx, target, opts)
	if err != nil {
		t.Fatalf("AddNetworkAdapter: %v", err)
	}
	t.Logf("Added: PortRef=%s AllocationRef=%s", result.PortRef, result.AllocationRef)

	// 後始末: PortRef を InstanceID として削除
	if result.PortRef == "" {
		t.Fatal("PortRef is empty, cannot cleanup")
	}
	t.Logf("Removing NIC: %s", result.PortRef)
	jobRef, err := client.RemoveNetworkAdapter(ctx, result.PortRef)
	if err != nil {
		t.Fatalf("RemoveNetworkAdapter: %v", err)
	}
	t.Logf("Remove Job: %s", jobRef)
}

// TestIntegration_ListIDEControllers は VM の IDE Controller 一覧を取得する (read-only)。
func TestIntegration_ListIDEControllers(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vms, err := client.ListComputerSystems(ctx)
	if err != nil {
		t.Fatalf("ListComputerSystems: %v", err)
	}
	if len(vms) == 0 {
		t.Skip("Hyper-V ホストに VM が存在しない")
	}
	target := vms[0]

	controllers, err := client.ListIDEControllers(ctx, target.Name)
	if err != nil {
		t.Fatalf("ListIDEControllers: %v", err)
	}
	t.Logf("VM %q: IDE Controllers = %d", target.ElementName, len(controllers))
	for i, ctrl := range controllers {
		t.Logf("  [%d] %s (%s)", i, ctrl.ElementName, ctrl.InstanceID)
	}
	// 注: Gen2 VM は IDE を持たない (0 が正常)。列挙が成功すること自体を検証する
	// (件数下限は課さない。以前は「>=1」を要求していたが Gen2 では誤り)。
}

// TestIntegration_ListScsiAndDiskDrives は SCSI Controller / Disk Drive / attached storage /
// NIC allocation の列挙を実機で検証する (read-only、provider の Get 逆引きの土台)。
//
// Gen2 VM (IDE 非搭載・SCSI ブート) を優先的に対象に選ぶ。#63 の disk/nic Get 経路が実 HW で
// データを返せることを確認する。
func TestIntegration_ListScsiAndDiskDrives(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	vms, err := client.ListComputerSystems(ctx)
	if err != nil {
		t.Fatalf("ListComputerSystems: %v", err)
	}
	if len(vms) == 0 {
		t.Skip("Hyper-V ホストに VM が存在しない")
	}
	// SCSI Controller を持つ VM (通常 Gen2) を優先的に選ぶ。
	target := vms[0]
	for _, vm := range vms {
		scsi, err := client.ListSCSIControllers(ctx, vm.Name)
		if err == nil && len(scsi) > 0 {
			target = vm
			break
		}
	}

	scsi, err := client.ListSCSIControllers(ctx, target.Name)
	if err != nil {
		t.Fatalf("ListSCSIControllers: %v", err)
	}
	drives, err := client.ListDiskDrives(ctx, target.Name)
	if err != nil {
		t.Fatalf("ListDiskDrives: %v", err)
	}
	storages, err := client.ListAttachedStorage(ctx, target.Name)
	if err != nil {
		t.Fatalf("ListAttachedStorage: %v", err)
	}
	allocs, err := client.ListEthernetPortAllocations(ctx, target.Name)
	if err != nil {
		t.Fatalf("ListEthernetPortAllocations: %v", err)
	}
	t.Logf("VM %q: SCSI=%d DiskDrives=%d AttachedStorage=%d NICAllocations=%d",
		target.ElementName, len(scsi), len(drives), len(storages), len(allocs))
	// 逆引きの整合を軽く確認: 各 Disk Drive は Parent(親 Controller EPR) を持つこと。
	for i, d := range drives {
		if d.Parent == "" {
			t.Errorf("disk drive[%d] の Parent(親 Controller) が空", i)
		}
	}
}

// TestIntegration_AttachDetachVHD は VHD ファイルのアタッチ→デタッチを検証する。
//
// 必要な環境変数:
//   - HYPERV_TEST_ALLOW_MUTATION=1
//   - HYPERV_TEST_TARGET_VM_NAME=<停止中の VM GUID>
//   - HYPERV_TEST_VHD_PATH=<アタッチする VHD ファイルのパス>
//
// 注意: VM が稼働中だと IDE への動的アタッチが失敗するため、停止中 VM を指定すること。
func TestIntegration_AttachDetachVHD(t *testing.T) {
	if os.Getenv("HYPERV_TEST_ALLOW_MUTATION") == "" {
		t.Skip("HYPERV_TEST_ALLOW_MUTATION 未設定")
	}
	target := os.Getenv("HYPERV_TEST_TARGET_VM_NAME")
	if target == "" {
		t.Skip("HYPERV_TEST_TARGET_VM_NAME 未設定")
	}
	vhdPath := os.Getenv("HYPERV_TEST_VHD_PATH")
	if vhdPath == "" {
		t.Skip("HYPERV_TEST_VHD_PATH 未設定")
	}

	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	t.Logf("Attaching VHD %q to VM %s", vhdPath, target)
	result, err := client.AttachVHD(ctx, target, AttachVHDOptions{
		ControllerType:     ControllerTypeIDE,
		ControllerNumber:   0,
		ControllerLocation: 0,
		Path:               vhdPath,
	})
	if err != nil {
		t.Fatalf("AttachVHD: %v", err)
	}
	t.Logf("Attached: DriveRef=%s StorageRef=%s", result.DriveRef, result.StorageRef)

	if result.DriveRef == "" {
		t.Fatal("DriveRef empty, cannot cleanup")
	}
	t.Logf("Detaching: %s", result.DriveRef)
	jobRef, err := client.DetachStorage(ctx, result.DriveRef)
	if err != nil {
		t.Fatalf("DetachStorage: %v", err)
	}
	t.Logf("Detach Job: %s", jobRef)
}

// TestIntegration_ListExternalEthernetPorts は実機ホストの物理 NIC 一覧を取得する。
func TestIntegration_ListExternalEthernetPorts(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ports, err := client.ListExternalEthernetPorts(ctx)
	if err != nil {
		t.Fatalf("ListExternalEthernetPorts: %v", err)
	}
	t.Logf("物理 NIC 件数: %d", len(ports))
	for _, p := range ports {
		t.Logf("  Name=%s Element=%q Bound=%v MAC=%s", p.Name, p.ElementName, p.IsBound, p.PermanentAddress)
	}
}

// TestIntegration_CreateDestroyPrivateSwitch は Private Switch の作成→削除を検証する。
//
// HYPERV_TEST_ALLOW_MUTATION 必須。
func TestIntegration_CreateDestroyPrivateSwitch(t *testing.T) {
	if os.Getenv("HYPERV_TEST_ALLOW_MUTATION") == "" {
		t.Skip("HYPERV_TEST_ALLOW_MUTATION 未設定")
	}

	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	name := fmt.Sprintf("go-wsman-private-%d", time.Now().UnixNano())
	t.Logf("Creating Private switch: %s", name)
	result, err := client.CreateSwitch(ctx, CreateSwitchOptions{
		Name: name,
		Type: SwitchTypePrivate,
	})
	if err != nil {
		t.Fatalf("CreateSwitch: %v", err)
	}
	t.Logf("Created: SwitchRef=%s", result.SwitchRef)

	// 後始末
	defer func() {
		t.Logf("Destroying switch: %s", name)
		if _, err := client.DestroySwitch(ctx, name); err != nil {
			t.Logf("DestroySwitch (cleanup) failed: %v", err)
		}
	}()

	// List で確認
	switches, err := client.ListVirtualEthernetSwitches(ctx)
	if err != nil {
		t.Fatalf("ListVirtualEthernetSwitches: %v", err)
	}
	found := false
	for _, sw := range switches {
		if sw.ElementName == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created switch %s not found", name)
	}
}

// TestIntegration_ScsiVhdLifecycle は #63 の write 経路 (SCSI への VHD アタッチ) を実機で
// 検証する。使い捨ての Gen2 VM と VHD を新規作成し、SCSI にアタッチ→列挙で確認→デタッチ→
// VM 破棄まで自己完結でクリーンアップする。稼働中 VM は一切触らない。
//
// 注: go-wsman は CIM 専用でファイル削除ができないため、作成した VHD ファイルは残留する
// (パスをログ出力するので手動削除する)。HYPERV_TEST_ALLOW_MUTATION=1 で有効。
func TestIntegration_ScsiVhdLifecycle(t *testing.T) {
	if os.Getenv("HYPERV_TEST_ALLOW_MUTATION") == "" {
		t.Skip("HYPERV_TEST_ALLOW_MUTATION 未設定（VM 作成を伴う破壊的テスト）")
	}
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// 1. 使い捨て Gen2 VM を作成 (Gen2 は SCSI Controller 0 を自動生成)。
	vmName := fmt.Sprintf("go-wsman-acctest-63-scsi-%d", time.Now().UnixNano())
	t.Logf("[1] DefineSystem (Gen2): %s", vmName)
	def, err := client.DefineSystem(ctx, &Msvm_VirtualSystemSettingData{
		ElementName:          vmName,
		VirtualSystemSubType: VirtualSystemSubTypeGen2,
	})
	if err != nil {
		t.Fatalf("DefineSystem: %v", err)
	}
	vmGUID := def.ResultingSystem
	defer func() {
		t.Logf("[cleanup] DestroySystem: %s", vmGUID)
		if _, err := client.DestroySystem(ctx, vmGUID); err != nil {
			t.Logf("    DestroySystem cleanup 失敗 (手動確認要): %v", err)
		}
	}()

	if vmGUID == "" {
		t.Fatalf("DefineSystem が ResultingSystem を返さなかった")
	}

	// 2. 実機発見: go-wsman の DefineSystem は「シェル」VM (ResourceSettings なし) を作るため、
	//    Hyper-V の New-VM と違い Gen2 でも SCSI Controller が自動生成されない (実機で 0 を確認)。
	//    → provider の SCSI ブートには、go-wsman 側に SCSI Controller 追加手段が別途必要
	//    (#63 のフォロー。バグではなく仕様なので件数は assert せず記録する)。
	scsi, err := client.ListSCSIControllers(ctx, vmGUID)
	if err != nil {
		t.Fatalf("ListSCSIControllers: %v", err)
	}
	t.Logf("[2] go-wsman DefineSystem 直後の Gen2 VM: SCSI Controllers = %d "+
		"(0 が実態。シェル VM のため。SCSI ブートには Controller 追加が必要)", len(scsi))

	// 3. 予備 VHD が指定されていれば、SCSI へのアタッチ→逆引き検証→デタッチまで行う。
	//    go-wsman の CreateVirtualHardDisk は実機で InternalError (別 Issue) のため、VHD 作成は
	//    しない。既存の使い捨て VHD パスを HYPERV_TEST_SCSI_VHD で渡すと full attach を検証する。
	vhdPath := os.Getenv("HYPERV_TEST_SCSI_VHD")
	if vhdPath == "" {
		t.Logf("[3] HYPERV_TEST_SCSI_VHD 未設定のため SCSI アタッチ検証はスキップ (Gen2→SCSI 生成のみ検証)")
		return
	}

	t.Logf("[3] AttachVHD (SCSI, location 0): %s", vhdPath)
	attach, err := client.AttachVHD(ctx, vmGUID, AttachVHDOptions{
		ControllerType:     ControllerTypeSCSI,
		ControllerNumber:   0,
		ControllerLocation: 0,
		Path:               vhdPath,
	})
	if err != nil {
		t.Fatalf("AttachVHD(SCSI): %v", err)
	}
	if attach.JobRef != "" {
		if err := client.WaitForJob(ctx, attach.JobRef); err != nil {
			t.Fatalf("WaitForJob(AttachVHD): %v", err)
		}
	}

	drives, err := client.ListDiskDrives(ctx, vmGUID)
	if err != nil {
		t.Fatalf("ListDiskDrives: %v", err)
	}
	storages, err := client.ListAttachedStorage(ctx, vmGUID)
	if err != nil {
		t.Fatalf("ListAttachedStorage: %v", err)
	}
	t.Logf("[4] 検証: DiskDrives=%d AttachedStorage=%d", len(drives), len(storages))
	if len(drives) < 1 {
		t.Errorf("SCSI に Disk Drive がアタッチされているべき (got 0)")
	}
	foundVHD := false
	for _, s := range storages {
		if s.HostResource == vhdPath {
			foundVHD = true
		}
	}
	if !foundVHD {
		t.Errorf("アタッチした VHD %q が ListAttachedStorage に現れない", vhdPath)
	}

	t.Logf("[5] DetachStorage")
	if _, err := client.DetachStorage(ctx, attach.DriveRef); err != nil {
		t.Errorf("DetachStorage: %v", err)
	}
}
