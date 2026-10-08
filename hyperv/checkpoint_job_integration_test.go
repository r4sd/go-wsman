//go:build integration

package hyperv

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestIntegration_ResolveCreatedCheckpoint は #125 を実機で検証する。
//
// 確かめること:
//  1. CreateSnapshot の ResultingSnapshot が実際に空で返ること (#125 の前提)
//  2. Job 完了前は ResolveCreatedCheckpoint が「見つからない」で返ること。
//     ただし**この呼び出し自体が数往復する**ので、その間に Job が完了すると
//     引けてしまう。前後の JobState を見て、**実行中のままだった時だけ**
//     「見つからない」を要求する (そうしないとレースで偽の失敗になる)
//  3. Job 完了後に引けること
//  4. 引けた InstanceID が ListVmCheckpoints の差分と一致すること
//     (既存の回避策との**独立な突合**。片方のバグでは両方同じ答えにならない)
//
// 使い捨て VM を作成→破棄する自己完結テスト (HYPERV_TEST_ALLOW_MUTATION gated)。
func TestIntegration_ResolveCreatedCheckpoint(t *testing.T) {
	if os.Getenv("HYPERV_TEST_ALLOW_MUTATION") == "" {
		t.Skip("HYPERV_TEST_ALLOW_MUTATION 未設定（VM 作成を伴う破壊的テスト）")
	}
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	vmName := fmt.Sprintf("go-wsman-acctest-125-%d", time.Now().UnixNano())
	def, err := client.DefineSystem(ctx, &Msvm_VirtualSystemSettingData{
		ElementName:          vmName,
		VirtualSystemSubType: VirtualSystemSubTypeGen2,
	})
	if err != nil {
		t.Fatalf("DefineSystem: %v", err)
	}
	vmGUID := def.ResultingSystem
	t.Cleanup(func() {
		// 🔴 ここで上の ctx を使ってはいけない。`defer cancel()` はテスト関数の
		// return 時に走り、t.Cleanup はその**後**に走るので ctx は必ず
		// canceled になっている。VM が実機に残る (実際に 1 度残した)。
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cleanupCancel()
		if _, err := client.DestroySystem(cleanupCtx, vmGUID); err != nil {
			t.Errorf("🔴 DestroySystem cleanup 失敗 (実機に VM が残る %s): %v", vmName, err)
		}
	})

	before, err := client.ListVmCheckpoints(ctx, vmGUID)
	if err != nil {
		t.Fatalf("ListVmCheckpoints(before): %v", err)
	}
	beforeIDs := map[string]bool{}
	for _, s := range before {
		beforeIDs[s.InstanceID] = true
	}

	res, err := client.CreateVmCheckpoint(ctx, vmGUID, SnapshotTypeFull)
	if err != nil {
		t.Fatalf("CreateVmCheckpoint: %v", err)
	}
	t.Logf("CreateVmCheckpoint: ReturnValue=%s JobRef=%q SnapshotRef=%q",
		res.ReturnValue, res.JobRef, res.SnapshotRef)

	// 1. #125 の前提。ここが埋まるようになったら Issue 自体が解決している。
	if res.SnapshotRef != "" {
		t.Errorf("ResultingSnapshot が埋まっている (%q)。#125 の前提が変わったので "+
			"ResolveCreatedCheckpoint の必要性を再評価すること", res.SnapshotRef)
	}
	if res.ReturnValue != "4096" {
		t.Fatalf("ReturnValue=%s (非同期 4096 を期待)。同期で返るなら本テストの前提が崩れる",
			res.ReturnValue)
	}

	// 2. Job 完了前は引けない。ただしレースがあるので JobState で場合分けする。
	stateBefore := jobStateOrZero(client, ctx, res.JobRef)
	earlyID, earlyErr := client.ResolveCreatedCheckpoint(ctx, res.JobRef)
	stateAfter := jobStateOrZero(client, ctx, res.JobRef)
	stillRunning := stateBefore != JobStateCompleted && stateAfter != JobStateCompleted
	switch {
	case earlyErr == nil && stillRunning:
		t.Errorf("Job 実行中 (before=%d after=%d) なのに引けた (%q)。"+
			"「実行中は Create 行が出ない」という観測が変わっている",
			stateBefore, stateAfter, earlyID)
	case earlyErr == nil:
		t.Logf("早い呼び出しの最中に Job が完了したため引けた (before=%d after=%d)。"+
			"このケースは失敗にしない", stateBefore, stateAfter)
	case stateBefore == JobStateCompleted:
		t.Errorf("Job が既に完了していた (before=%d) のに引けない: %v", stateBefore, earlyErr)
	case !strings.Contains(earlyErr.Error(), "見つからない"):
		t.Errorf("Job 完了前のエラーが想定と違う: %v", earlyErr)
	}

	if err := client.WaitForJob(ctx, res.JobRef); err != nil {
		t.Fatalf("WaitForJob: %v", err)
	}

	// 3. Job 完了後に引ける。
	got, err := client.ResolveCreatedCheckpoint(ctx, res.JobRef)
	if err != nil {
		t.Fatalf("ResolveCreatedCheckpoint: %v", err)
	}
	t.Logf("ResolveCreatedCheckpoint = %q", got)
	if !strings.HasPrefix(got, "Microsoft:") {
		t.Errorf("InstanceID が %q。スナップショットの VSSD は Microsoft:<GUID> の形のはず", got)
	}

	// 4. 既存の回避策 (前後の差分) と突合する。
	after, err := client.ListVmCheckpoints(ctx, vmGUID)
	if err != nil {
		t.Fatalf("ListVmCheckpoints(after): %v", err)
	}
	var diff []string
	for _, s := range after {
		if !beforeIDs[s.InstanceID] {
			diff = append(diff, s.InstanceID)
		}
	}
	if len(diff) != 1 {
		t.Fatalf("前後の差分が %d 件 (1 件を期待): %v", len(diff), diff)
	}
	if diff[0] != got {
		t.Errorf("Job 経由 %q と前後差分 %q が一致しない。どちらかが誤り", got, diff[0])
	}

	// 後片付け (VM 破棄でも消えるが、DestroySnapshot 自体も通ることを確かめる)。
	//
	// 🔴 Job を待つ。待たずに t.Cleanup の DestroySystem に進むと
	// スナップショット削除が実行中で ReturnValue=32775 (Invalid state) になり、
	// VM が実機に残る (実際に 1 度残した)。
	snapJob, err := client.DestroyVmCheckpoint(ctx, got)
	if err != nil {
		t.Errorf("DestroyVmCheckpoint(%q): %v", got, err)
	} else if err := client.WaitForJob(ctx, snapJob); err != nil {
		t.Errorf("DestroyVmCheckpoint の Job 待ち: %v", err)
	}
}

// jobStateOrZero は Job の JobState を返す。取れなければ 0 を返す
// (判定を壊さないため。0 は JobStateCompleted ではないので「実行中扱い」に倒れる)。
func jobStateOrZero(c *Client, ctx context.Context, jobRef string) uint16 {
	job, err := c.getJob(ctx, msvmConcreteJobURI, jobRef)
	if err != nil {
		return 0
	}
	return job.JobState
}
