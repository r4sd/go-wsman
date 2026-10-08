//go:build integration

package hyperv

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestIntegration_UpdateVmWritesDataRoots は #195 を実機で検証する。
//
// clearReadOnlyForModify が SnapshotDataRoot / SwapFileDataRoot をクリアしていた間、
// この 2 つの変更は ModifySystemSettings に届かず**黙って捨てられていた**。
// ユニットテストは「関数がクリアしないこと」しか見られないので、
// **実際に書けること**はここで固定する。
//
// 使い捨て VM を作成→破棄する自己完結テスト (HYPERV_TEST_ALLOW_MUTATION gated)。
func TestIntegration_UpdateVmWritesDataRoots(t *testing.T) {
	if os.Getenv("HYPERV_TEST_ALLOW_MUTATION") == "" {
		t.Skip("HYPERV_TEST_ALLOW_MUTATION 未設定（VM 作成を伴う破壊的テスト）")
	}
	client := getIntegrationClient(t)
	ctx := context.Background()

	vmName := fmt.Sprintf("go-wsman-acctest-195-%d", time.Now().UnixNano())
	def, err := client.DefineSystem(ctx, &Msvm_VirtualSystemSettingData{
		ElementName:          vmName,
		VirtualSystemSubType: VirtualSystemSubTypeGen2,
	})
	if err != nil {
		t.Fatalf("DefineSystem: %v", err)
	}
	vmGUID := def.ResultingSystem
	t.Cleanup(func() {
		jobRef, err := client.DestroySystem(ctx, vmGUID)
		if err != nil {
			t.Errorf("🔴 DestroySystem cleanup 失敗 (実機に VM が残る %s): %v", vmName, err)
			return
		}
		if err := client.WaitForJob(ctx, jobRef); err != nil {
			t.Errorf("🔴 DestroySystem の Job 待ちが失敗 (実機に VM が残る %s): %v", vmName, err)
		}
	})

	before, err := client.GetSystemSettingData(ctx, vmGUID)
	if err != nil {
		t.Fatalf("GetSystemSettingData(before): %v", err)
	}

	// 🔴 既定値と**違う値**を入れる。既定値を書くと「書けた」と「元のまま」が
	// 区別できない (DefineSystem 直後は両方ともホスト既定の同じパスになる)。
	wantSnap := `C:\go-wsman-acctest-195-snap`
	wantSwap := `C:\go-wsman-acctest-195-swap`
	if before.SnapshotDataRoot == wantSnap || before.SwapFileDataRoot == wantSwap {
		t.Fatalf("既定値が検査値と同じ (snap=%q swap=%q)。値を変えること",
			before.SnapshotDataRoot, before.SwapFileDataRoot)
	}
	// 🔴 **Hyper-V は指定パスにディレクトリを作る。** go-wsman は CIM 専用で
	// ファイル削除ができないため、VM を破棄してもこの 2 つは**ホストに残る**。
	// パスをログに出す (VHD の統合テストと同じ扱い)。
	t.Logf("🔴 ホストに残るディレクトリ (手動削除要): %s / %s", wantSnap, wantSwap)

	jobRef, err := client.UpdateVm(ctx, &Msvm_VirtualSystemSettingData{
		InstanceID:       before.InstanceID,
		SnapshotDataRoot: wantSnap,
		SwapFileDataRoot: wantSwap,
	})
	if err != nil {
		t.Fatalf("UpdateVm: %v", err)
	}
	if err := client.WaitForJob(ctx, jobRef); err != nil {
		t.Fatalf("UpdateVm の Job 待ち: %v", err)
	}

	after, err := client.GetSystemSettingData(ctx, vmGUID)
	if err != nil {
		t.Fatalf("GetSystemSettingData(after): %v", err)
	}
	if after.SnapshotDataRoot != wantSnap {
		t.Errorf("SnapshotDataRoot = %q, want %q (before=%q)。"+
			"clearReadOnlyForModify がクリアに戻っていないか確認すること",
			after.SnapshotDataRoot, wantSnap, before.SnapshotDataRoot)
	}
	if after.SwapFileDataRoot != wantSwap {
		t.Errorf("SwapFileDataRoot = %q, want %q (before=%q)。"+
			"clearReadOnlyForModify がクリアに戻っていないか確認すること",
			after.SwapFileDataRoot, wantSwap, before.SwapFileDataRoot)
	}
}
