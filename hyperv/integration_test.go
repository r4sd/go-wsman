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
