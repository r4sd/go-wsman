//go:build integration

package hyperv

import (
	"context"
	"os"
	"testing"
)

// TestRealDynamicMemoryExplicitFalse は DynamicMemoryEnabled の明示 false を実機で検証する (#149)。
//
// ホスト既定は true。利用側 (provider の static_memory=true) は false を要求するので、
// この遷移は新規 VM のたびに踏む。値型のままだとゼロ値スキップで黙殺されていた。
//
// UpdateVm (ModifyResourceSettings) と DefineSystem + SetMemorySettings で
// 受理されるかは別問題なので、両経路を叩く。
func TestRealDynamicMemoryExplicitFalse(t *testing.T) {
	if os.Getenv("WSMAN_TEST_ALLOW_MUTATION") == "" {
		t.Skip("WSMAN_TEST_ALLOW_MUTATION 未設定（VM 作成を伴う破壊的テスト）")
	}
	c := getIntegrationClient(t)
	ctx := context.Background()
	guid := defineThrowawayVM(t, c, ctx, "gw-p1-dynmem")

	mem, err := c.GetMemorySettings(ctx, guid)
	if err != nil {
		t.Fatalf("GetMemorySettings: %v", err)
	}
	if mem.DynamicMemoryEnabled == nil {
		t.Fatalf("DynamicMemoryEnabled が read で nil")
	}
	t.Logf("① 作成直後 (ホスト既定): DynamicMemoryEnabled=%v", *mem.DynamicMemoryEnabled)
	if !*mem.DynamicMemoryEnabled {
		t.Skip("ホスト既定が false のため true→false の遷移を検証できない")
	}

	f := false
	mem.DynamicMemoryEnabled = &f
	jobRef, err := c.SetMemorySettings(ctx, mem)
	if err != nil {
		t.Fatalf("SetMemorySettings: %v", err)
	}
	if err := c.WaitForJob(ctx, jobRef); err != nil {
		t.Fatalf("WaitForJob: %v", err)
	}

	after, err := c.GetMemorySettings(ctx, guid)
	if err != nil {
		t.Fatalf("GetMemorySettings (書き込み後): %v", err)
	}
	t.Logf("② &false 送信後: DynamicMemoryEnabled=%v", *after.DynamicMemoryEnabled)
	if *after.DynamicMemoryEnabled {
		t.Errorf("🔴 明示的な false が黙殺されている")
	}
	t.Logf("🎯 判定: DynamicMemoryEnabled の &false が実機に反映される")
}

// TestRealSecureBootExplicitFalse は SecureBoot の明示 false を実機で検証する (#149)。
//
// Gen2 のホスト既定は true。Linux ゲストでは false にするのが一般的。
func TestRealSecureBootExplicitFalse(t *testing.T) {
	if os.Getenv("WSMAN_TEST_ALLOW_MUTATION") == "" {
		t.Skip("WSMAN_TEST_ALLOW_MUTATION 未設定（VM 作成を伴う破壊的テスト）")
	}
	c := getIntegrationClient(t)
	ctx := context.Background()
	guid := defineThrowawayVMGen(t, c, ctx, "gw-p1-secureboot", VirtualSystemSubTypeGen2)

	before, err := c.GetSystemSettingData(ctx, guid)
	if err != nil {
		t.Fatalf("GetSystemSettingData: %v", err)
	}
	if before.SecureBoot == nil {
		t.Fatalf("SecureBoot が read で nil")
	}
	t.Logf("① 作成直後 (Gen2 既定): SecureBoot=%v", *before.SecureBoot)
	if !*before.SecureBoot {
		t.Skip("ホスト既定が false のため true→false の遷移を検証できない")
	}

	f := false
	jobRef, err := c.UpdateVm(ctx, &Msvm_VirtualSystemSettingData{
		InstanceID: before.InstanceID,
		SecureBoot: &f,
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
	t.Logf("② &false 送信後: SecureBoot=%v", *after.SecureBoot)
	if *after.SecureBoot {
		t.Errorf("🔴 明示的な false が黙殺されている")
	}
	t.Logf("🎯 判定: SecureBoot の &false が実機に反映される")
}

// defineThrowawayVM は使い捨て Gen1 VM を作り、t.Cleanup で削除する。
func defineThrowawayVM(t *testing.T, c *Client, ctx context.Context, name string) string {
	t.Helper()
	return defineThrowawayVMGen(t, c, ctx, name, VirtualSystemSubTypeGen1)
}

// defineThrowawayVMGen は世代を指定して使い捨て VM を作る。
//
// 削除は **VM GUID** で行う (ElementName を渡すと ReturnValue=32773 になり、
// 実機に VM が残る)。
func defineThrowawayVMGen(t *testing.T, c *Client, ctx context.Context, name, subType string) string {
	t.Helper()
	res, err := c.DefineSystem(ctx, &Msvm_VirtualSystemSettingData{
		ElementName: name, VirtualSystemSubType: subType,
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
	return guid
}

// TestRealGen1SecureBootSend は Gen1 VM へ SecureBootEnabled=false を送っても
// 拒否されないことを確認する (#149)。
//
// MOF は「Secure boot can only be enabled for generation 2」と書いており、Gen1 でも
// read は FALSE を返す。ポインタ化により「GetSystemSettingData → 編集 → UpdateVm」の
// パターンでは Gen1 でも SecureBootEnabled=false が毎回送られることになるため、
// 実機が拒否しないかを押さえておく。
//
// 現時点で go-wsman / provider とも全体コピー送信をする経路は無いので実害は無いが、
// 将来そのパターンを書いた時に踏む。
func TestRealGen1SecureBootSend(t *testing.T) {
	if os.Getenv("WSMAN_TEST_ALLOW_MUTATION") == "" {
		t.Skip("WSMAN_TEST_ALLOW_MUTATION 未設定（VM 作成を伴う破壊的テスト）")
	}
	c := getIntegrationClient(t)
	ctx := context.Background()
	guid := defineThrowawayVM(t, c, ctx, "gw-p1-gen1-sb")

	before, err := c.GetSystemSettingData(ctx, guid)
	if err != nil {
		t.Fatalf("GetSystemSettingData: %v", err)
	}
	if before.SecureBoot == nil {
		t.Logf("① Gen1 は SecureBootEnabled を返さない (nil) → 送信されないので無害")
		return
	}
	t.Logf("① Gen1 作成直後: SecureBoot=%v", *before.SecureBoot)

	f := false
	jobRef, err := c.UpdateVm(ctx, &Msvm_VirtualSystemSettingData{
		InstanceID: before.InstanceID,
		SecureBoot: &f,
	})
	if err != nil {
		t.Fatalf("🔴 Gen1 への SecureBootEnabled=false 送信が拒否された: %v", err)
	}
	if jobRef != "" {
		if err := c.WaitForJob(ctx, jobRef); err != nil {
			t.Fatalf("🔴 Gen1 への SecureBootEnabled=false 送信でジョブが失敗: %v", err)
		}
	}
	t.Logf("🎯 判定: Gen1 へ SecureBootEnabled=false を送っても拒否されない")
}
