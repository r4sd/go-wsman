//go:build integration

package hyperv

import (
	"context"
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

	client, err := NewClient(endpoint, wsman.WithNTLM(username, password))
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
//   - HYPERV_TEST_VHD_PATH: 既存 VHD ファイルのフルパス（例: "C:\\VMs\\test.vhdx"）
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
