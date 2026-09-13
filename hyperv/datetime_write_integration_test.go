//go:build integration

package hyperv

import (
	"context"
	"os"
	"testing"
)

// TestRealDatetimeWrite は datetime(interval) プロパティの書き込みが通ることを実機で検証する (#119)。
//
// 実機で確定した非対称 (2026-09-13、使い捨て VM で 2x2 を全数試行):
//
//	                              TYPE="string"   TYPE="datetime"
//	ISO 8601 "P0DT0H45M0S"            ErrorCode=32768   ErrorCode=32768
//	CIM native "0000000045...:000"    ErrorCode=32768   ✅ 成功
//
// 型と値の **両方** を直さないと通らない。read は ISO 8601 を返すので、
// 往復 (read → write → read) が成立することをここで押さえる。
func TestRealDatetimeWrite(t *testing.T) {
	if os.Getenv("WSMAN_TEST_ALLOW_MUTATION") == "" {
		t.Skip("WSMAN_TEST_ALLOW_MUTATION 未設定（VM 作成を伴う破壊的テスト）")
	}
	c := getIntegrationClient(t)
	ctx := context.Background()

	res, err := c.DefineSystem(ctx, &Msvm_VirtualSystemSettingData{
		ElementName:          "gw-datetime-write",
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
	t.Logf("① 既定値: timeout=%q delay=%q",
		before.AutomaticCriticalErrorActionTimeout, before.AutomaticStartupActionDelay)

	const wantTimeout, wantDelay = "P0DT0H45M0S", "P0DT0H1M30S"
	jobRef, err := c.UpdateVm(ctx, &Msvm_VirtualSystemSettingData{
		InstanceID:                          before.InstanceID,
		AutomaticCriticalErrorActionTimeout: wantTimeout,
		AutomaticStartupActionDelay:         wantDelay,
	})
	if err != nil {
		t.Fatalf("🔴 UpdateVm: %v", err)
	}
	if jobRef != "" {
		if err := c.WaitForJob(ctx, jobRef); err != nil {
			t.Fatalf("🔴 WaitForJob: %v", err)
		}
	}

	after, err := c.GetSystemSettingData(ctx, guid)
	if err != nil {
		t.Fatalf("GetSystemSettingData (書き込み後): %v", err)
	}
	t.Logf("② 書き込み後: timeout=%q delay=%q",
		after.AutomaticCriticalErrorActionTimeout, after.AutomaticStartupActionDelay)

	if after.AutomaticCriticalErrorActionTimeout != wantTimeout {
		t.Errorf("🔴 timeout: got %q, want %q", after.AutomaticCriticalErrorActionTimeout, wantTimeout)
	}
	if after.AutomaticStartupActionDelay != wantDelay {
		t.Errorf("🔴 delay: got %q, want %q", after.AutomaticStartupActionDelay, wantDelay)
	}
	t.Logf("🎯 判定: datetime(interval) の read → write → read が往復する")
}
