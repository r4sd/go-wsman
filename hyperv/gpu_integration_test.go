//go:build integration

package hyperv

import (
	"context"
	"testing"
	"time"
)

// TestIntegration_ListGpuAdapters は実機で GPU パーティション取得が fault しないことを検証する。
//
// #77 のクラックス: GPU-P 非対応 (GPU 未使用) ホストで Msvm_GpuPartitionSettingData を列挙すると、
// enumerate はホスト能力定義 (Microsoft:Definition\...) を返すが VM 割当は 0 件になる。
// ListGpuAdapters が能力定義を除外して各 VM で空スライス・no-error を返すことを実機で確認する
// (2026-07-16 プローブで enumerate=4 件の能力定義を確認済み。homelab は GPU 未使用)。
func TestIntegration_ListGpuAdapters(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	vms, err := client.ListComputerSystems(ctx)
	if err != nil {
		t.Fatalf("ListComputerSystems failed: %v", err)
	}
	if len(vms) == 0 {
		t.Skip("Hyper-V ホストに VM が存在しない")
	}

	for _, vm := range vms {
		gpus, err := client.ListGpuAdapters(ctx, vm.Name)
		if err != nil {
			// クラックス: 能力定義しかない GPU-less ホストで fault してはいけない。
			t.Errorf("ListGpuAdapters(%q / %s) failed: %v", vm.ElementName, vm.Name, err)
			continue
		}
		t.Logf("VM %q (%s): GPU partition = %d 件", vm.ElementName, vm.Name, len(gpus))
		for _, g := range gpus {
			// 能力定義 (Microsoft:Definition\...) が混入していないこと。
			if !hasPrefix(g.InstanceID, "Microsoft:"+vm.Name) {
				t.Errorf("能力定義または別 VM の InstanceID が混入: %q", g.InstanceID)
			}
		}
	}
}

// hasPrefix は strings.HasPrefix の薄いラッパー (テスト内での明示用)。
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
