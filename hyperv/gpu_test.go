package hyperv

import (
	"context"
	"strings"
	"testing"
)

// TestClient_ListGpuAdapters は対象 VM に割り当てられた GPU パーティションだけを返し、
// ホスト能力定義 (Microsoft:Definition\...) と別 VM の割当を除外することを検証する。
//
// golden は 2026-07-16 の実機プローブで確認した enumerate の中身をモデル化している:
// Definition\Default / Definition\Maximum (能力定義) + 対象 VM の割当 + 別 VM の割当。
func TestClient_ListGpuAdapters(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_idecontroller.xml")
	pull := loadGolden(t, "pull_response_gpupartition_mixed.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListGpuAdapters(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("ListGpuAdapters: %v", err)
	}
	// 能力定義 2 件・別 VM 1 件を除外し、対象 VM の割当 1 件だけ。
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1 (能力定義・別 VM を含めてはいけない)", len(got))
	}
	if !strings.Contains(got[0].InstanceID, "11111111-aaaa-bbbb-cccc-000000000001") {
		t.Errorf("InstanceID: got %q, want 対象 VM 割当", got[0].InstanceID)
	}
	if got[0].MinPartitionVRAM != 80000000 {
		t.Errorf("MinPartitionVRAM: got %d, want 80000000", got[0].MinPartitionVRAM)
	}
	if got[0].MaxPartitionVRAM != 100000000 {
		t.Errorf("MaxPartitionVRAM: got %d, want 100000000", got[0].MaxPartitionVRAM)
	}
	if got[0].OptimalPartitionVRAM != 90000000 {
		t.Errorf("OptimalPartitionVRAM: got %d, want 90000000", got[0].OptimalPartitionVRAM)
	}
	// uint64 の上限値 (Encode/Decode/Compute の Max) が桁落ちなく Unmarshal できること。
	if got[0].MaxPartitionCompute != 18446744073709551615 {
		t.Errorf("MaxPartitionCompute: got %d, want uint64 max", got[0].MaxPartitionCompute)
	}

	// Hyper-V は WQL フィルタ列挙を拒否するため、Enumerate は無フィルタで送ること (#80)。
	if strings.Contains(bodies[0], "Filter") || strings.Contains(bodies[0], "SELECT") {
		t.Errorf("enumerate should be unfiltered (no WQL Filter); body: %s", bodies[0])
	}
}

// TestClient_ListGpuAdapters_Empty は GPU パーティション未割当の VM (homelab 相当) で
// 空スライス・no-error を返すことを検証する。能力定義しか存在しない列挙結果に対し、
// 存在しない VM_GUID で絞ると 0 件になる。
func TestClient_ListGpuAdapters_Empty(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_idecontroller.xml")
	pull := loadGolden(t, "pull_response_gpupartition_mixed.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListGpuAdapters(context.Background(), "99999999-ffff-ffff-ffff-999999999999")
	if err != nil {
		t.Fatalf("ListGpuAdapters: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len: got %d, want 0 (GPU 未割当 VM は空スライス)", len(got))
	}
}

// TestClient_ListGpuAdapters_EmptyVMGUID は空 vmGUID を弾くことを検証する。
func TestClient_ListGpuAdapters_EmptyVMGUID(t *testing.T) {
	client, _ := NewClient("https://example.invalid:5986/wsman")
	if _, err := client.ListGpuAdapters(context.Background(), ""); err == nil {
		t.Fatal("空 vmGUID はエラーになるべき")
	}
}
