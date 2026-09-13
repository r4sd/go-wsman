//go:build integration

package hyperv

import (
	"context"
	"os"
	"testing"
)

// TestRealExplicitFalse は明示的な false が実機に反映されることを検証する (#135)。
//
// 実機確認 (2026-09-09):
//
//	ModifySystemSettings で AutomaticSnapshotsEnabled=true  を送る → 反映される
//	ModifySystemSettings で AutomaticSnapshotsEnabled=false を送る → 黙殺 (true のまま)
//
// MOF 上 Read/write なので Hyper-V が拒否しているのではなく、こちらが
// marshalEmbeddedInstance のゼロ値スキップで **送っていなかった**。
//
// ホスト既定 (クライアント Hyper-V) は true、利用側の既定は false なので
// この遷移は新規 VM のたびに必ず要求される。
func TestRealExplicitFalse(t *testing.T) {
	if os.Getenv("WSMAN_TEST_ALLOW_MUTATION") == "" {
		t.Skip("WSMAN_TEST_ALLOW_MUTATION 未設定（VM 作成を伴う破壊的テスト）")
	}
	c := getIntegrationClient(t)
	ctx := context.Background()

	res, err := c.DefineSystem(ctx, &Msvm_VirtualSystemSettingData{
		ElementName:          "gw-explicit-zero",
		VirtualSystemSubType: VirtualSystemSubTypeGen1,
	})
	if err != nil {
		t.Fatalf("DefineSystem: %v", err)
	}
	guid := res.ResultingSystem
	t.Cleanup(func() {
		jobRef, err := c.DestroySystem(ctx, guid)
		if err != nil {
			t.Errorf("🔴 cleanup DestroySystem (実機に VM が残る): %v", err)
			return
		}
		if jobRef != "" {
			if err := c.WaitForJob(ctx, jobRef); err != nil {
				t.Errorf("🔴 cleanup WaitForJob: %v", err)
			}
		}
	})

	before, err := c.GetSystemSettingData(ctx, guid)
	if err != nil {
		t.Fatalf("GetSystemSettingData: %v", err)
	}
	if before.AutomaticSnapshotsEnabled == nil {
		t.Fatalf("AutomaticSnapshotsEnabled が read で nil (このホストは当該プロパティを返さない)")
	}
	t.Logf("① 作成直後 (ホスト既定): AutomaticSnapshotsEnabled=%v", *before.AutomaticSnapshotsEnabled)
	if !*before.AutomaticSnapshotsEnabled {
		t.Skip("ホスト既定が false のため true→false の遷移を検証できない (Windows Server ホスト)")
	}

	f := false
	jobRef, err := c.UpdateVm(ctx, &Msvm_VirtualSystemSettingData{
		InstanceID:                before.InstanceID,
		AutomaticSnapshotsEnabled: &f,
	})
	if err != nil {
		t.Fatalf("UpdateVm: %v", err)
	}
	if jobRef != "" {
		if err := c.WaitForJob(ctx, jobRef); err != nil {
			t.Fatalf("WaitForJob: %v", err)
		}
	}

	after, err := c.GetSystemSettingData(ctx, guid)
	if err != nil {
		t.Fatalf("GetSystemSettingData (書き込み後): %v", err)
	}
	if after.AutomaticSnapshotsEnabled == nil {
		t.Fatalf("書き込み後の read が nil")
	}
	t.Logf("② &false 送信後: AutomaticSnapshotsEnabled=%v", *after.AutomaticSnapshotsEnabled)
	if *after.AutomaticSnapshotsEnabled {
		t.Errorf("🔴 明示的な false が黙殺されている (#135 が直っていない)")
	}
	t.Logf("🎯 判定: &false が実機に反映される")
}
