//go:build integration

package hyperv

// #123 のプローブ: CreateSnapshot はチェックポイント名 (ElementName) を設定する経路を持たない
// (SnapshotSettings の型 Msvm_VirtualSystemSnapshotSettingData に ElementName プロパティが
// 無いことを MOF で確認済み)。唯一考えられる代替が「作成後に ModifySystemSettings で
// スナップショットの SettingData の ElementName を書き換える」ことだが、VSMS が
// VirtualSystemType=Snapshot:Realized の SettingData を受理するかは一次資料からは判断できない。
// 本テストで実機に問うて確定させる。
//
// 実行:
//
//	WSMAN_ENDPOINT=https://10.0.0.100:5986/wsman WSMAN_USERNAME=terraform \
//	WSMAN_PASSWORD=... go test -tags=integration -v ./hyperv/ -run TestCheckpointRename

import (
	"context"
	"errors"
	"testing"

	"github.com/r4sd/go-wsman/wsman"
)

// logFaultDetail は SOAP Fault の Detail (Hyper-V 側の ErrorCode/説明が入る) をログに出す。
// t.Fatalf の後には実行されないため、Fatal より先に呼ぶこと。
func logFaultDetail(t *testing.T, err error) {
	t.Helper()
	var f *wsman.Fault
	if errors.As(err, &f) && f.Detail != "" {
		t.Logf("fault detail: %s", f.Detail)
	}
}

func TestCheckpointRenameProbe(t *testing.T) {
	c := sweepClient(t)
	ctx := context.Background()

	const testVM = "tf-wsman-test-checkpoint"
	cleanup := func() {
		cs, err := c.FindComputerSystemByElementName(ctx, testVM)
		if err != nil {
			return // 不在
		}
		// チェックポイントが残っていると VM 削除に失敗するため先に消す。
		if cps, e := c.ListVmCheckpoints(ctx, cs.Name); e == nil {
			for _, cp := range cps {
				if jr, e := c.DestroyVmCheckpoint(ctx, cp.InstanceID); e == nil {
					_ = c.WaitForJob(ctx, jr)
				}
			}
		}
		if cs.EnabledState != EnabledStateDisabled {
			if jr, e := c.TurnOffVM(ctx, cs.Name); e == nil {
				_ = c.WaitForJob(ctx, jr)
			}
		}
		if jr, e := c.DestroySystem(ctx, cs.Name); e == nil {
			_ = c.WaitForJob(ctx, jr)
		}
	}
	t.Cleanup(cleanup)
	cleanup() // 前回残骸を消す

	// --- 1. 使い捨て VM を作成 ---
	sd := &Msvm_VirtualSystemSettingData{
		ElementName:          testVM,
		VirtualSystemSubType: VirtualSystemSubTypeGen2,
	}
	res, err := c.DefineSystem(ctx, sd)
	if err != nil {
		t.Fatalf("DefineSystem: %v", err)
	}
	if err := c.WaitForJob(ctx, res.JobRef); err != nil {
		t.Fatalf("WaitForJob(DefineSystem): %v", err)
	}
	guid := res.ResultingSystem
	t.Logf("created VM GUID=%s", guid)

	// --- 2. チェックポイントを作成 (名前は指定できない = #123 の前提を実機で再確認) ---
	cp, err := c.CreateVmCheckpoint(ctx, guid, SnapshotTypeFull)
	if err != nil {
		logFaultDetail(t, err)
		t.Fatalf("CreateVmCheckpoint: %v", err)
	}
	if err := c.WaitForJob(ctx, cp.JobRef); err != nil {
		t.Fatalf("WaitForJob(CreateVmCheckpoint): %v", err)
	}
	t.Logf("CreateVmCheckpoint: ReturnValue=%s SnapshotRef=%q JobRef=%q", cp.ReturnValue, cp.SnapshotRef, cp.JobRef)

	// --- 3. 作成されたチェックポイントを列挙し、既定の ElementName を観測する ---
	cps, err := c.ListVmCheckpoints(ctx, guid)
	if err != nil {
		t.Fatalf("ListVmCheckpoints: %v", err)
	}
	if len(cps) != 1 {
		t.Fatalf("ListVmCheckpoints: got %d checkpoints, want 1", len(cps))
	}
	original := cps[0]
	t.Logf("🎯 既定の ElementName = %q (InstanceID=%q)", original.ElementName, original.InstanceID)

	// --- 4. RenameVmCheckpoint で ElementName を任意の名前に変更する (#123 の答え) ---
	const newName = "renamed-by-probe"
	jobRef, err := c.RenameVmCheckpoint(ctx, original.InstanceID, newName)
	if err != nil {
		t.Logf("🔴 判定: ModifySystemSettings はスナップショットの SettingData を受理しない: %v", err)
		logFaultDetail(t, err)
		t.Fatal("リネーム経路なし = #123 は別の手段を探す必要がある")
	}
	if err := c.WaitForJob(ctx, jobRef); err != nil {
		t.Logf("🔴 判定: 呼び出しは通ったが Job が失敗: %v", err)
		t.Fatal("リネーム経路なし = #123 は別の手段を探す必要がある")
	}

	// --- 5. 実際に名前が変わったかを読み直して確認 (成功報告だけを信じない) ---
	after, err := c.ListVmCheckpoints(ctx, guid)
	if err != nil {
		t.Fatalf("ListVmCheckpoints (after): %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("ListVmCheckpoints (after): got %d checkpoints, want 1", len(after))
	}
	if after[0].ElementName != newName {
		t.Fatalf("🔴 判定: Job は成功したが ElementName が変わっていない (黙殺): got %q, want %q",
			after[0].ElementName, newName)
	}
	t.Logf("🎯 判定: ModifySystemSettings でスナップショットのリネームが可能 (ElementName=%q)", after[0].ElementName)
}
