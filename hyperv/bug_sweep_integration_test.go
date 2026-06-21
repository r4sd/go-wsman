//go:build integration

package hyperv

// Phase D バグスイープ: 各 go-wsman 操作を独立サブテストで実機に対して叩き、
// 不具合を一括で洗い出す。各サブテストは t.Errorf (非 Fatal) で落ちても次へ進むため、
// 1 回の実行で全操作の成否マップが得られる。read 系は既存 VM 対象 (read-only で安全)、
// write 系はテスト VM (tf-wsman-test-sweep) を作成・破棄する (cleanup 徹底)。
//
// 実行:
//
//	WSMAN_ENDPOINT=https://10.0.0.100:5986/wsman WSMAN_USERNAME=terraform \
//	WSMAN_PASSWORD=... go test -tags=integration -v ./hyperv/ -run TestBugSweep

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/r4sd/go-wsman/wsman"
)

func sweepClient(t *testing.T) *Client {
	t.Helper()
	endpoint := os.Getenv("WSMAN_ENDPOINT")
	username := os.Getenv("WSMAN_USERNAME")
	password := os.Getenv("WSMAN_PASSWORD")
	if endpoint == "" || username == "" || password == "" {
		t.Skip("WSMAN_ENDPOINT / WSMAN_USERNAME / WSMAN_PASSWORD 未設定のためスキップ")
	}
	// homelab は自己署名証明書のため InsecureSkipVerify を併用。
	client, err := NewClient(endpoint, wsman.WithNTLM(username, password), wsman.WithInsecureSkipVerify())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestBugSweep_WsmanOperations(t *testing.T) {
	c := sweepClient(t)
	ctx := context.Background()

	// --- 前提: 既存 VM を 1 つ取得 (read-only テスト対象) ---
	vms, err := c.ListComputerSystems(ctx)
	if err != nil {
		t.Fatalf("ListComputerSystems (前提) failed: %v", err)
	}
	var sampleName, sampleGUID string
	for _, vm := range vms {
		// guest VM のみ対象 (管理ホストは Name==ElementName==ホスト名)。
		// guest は Name=GUID ≠ ElementName=表示名。
		if vm.ElementName != "" && vm.Name != "" && vm.Name != vm.ElementName {
			sampleName, sampleGUID = vm.ElementName, vm.Name
			break
		}
	}
	if sampleName == "" {
		t.Skip("read テスト用の guest VM が見つからない")
	}
	t.Logf("read テスト対象 VM: name=%q guid=%q (既存 %d VM)", sampleName, sampleGUID, len(vms))

	// --- READ 系 (既存 VM 対象、read-only で安全) ---
	t.Run("read/FindComputerSystemByElementName", func(t *testing.T) {
		if _, err := c.FindComputerSystemByElementName(ctx, sampleName); err != nil {
			t.Errorf("FAIL: %v", err)
		}
	})
	t.Run("read/GetSystemSettingData", func(t *testing.T) {
		if _, err := c.GetSystemSettingData(ctx, sampleGUID); err != nil {
			t.Errorf("FAIL: %v", err)
		}
	})
	t.Run("read/GetMemorySettings", func(t *testing.T) {
		if _, err := c.GetMemorySettings(ctx, sampleGUID); err != nil {
			t.Errorf("FAIL: %v", err)
		}
	})
	t.Run("read/GetProcessorSettings", func(t *testing.T) {
		if _, err := c.GetProcessorSettings(ctx, sampleGUID); err != nil {
			t.Errorf("FAIL: %v", err)
		}
	})
	t.Run("read/ListNetworkAdapters", func(t *testing.T) {
		if _, err := c.ListNetworkAdapters(ctx, sampleGUID); err != nil {
			t.Errorf("FAIL: %v", err)
		}
	})
	t.Run("read/ListAttachedStorage", func(t *testing.T) {
		if _, err := c.ListAttachedStorage(ctx, sampleGUID); err != nil {
			t.Errorf("FAIL: %v", err)
		}
	})
	t.Run("read/ListIDEControllers", func(t *testing.T) {
		if _, err := c.ListIDEControllers(ctx, sampleGUID); err != nil {
			t.Errorf("FAIL: %v", err)
		}
	})

	// --- WRITE 系 (テスト VM、cleanup 徹底) ---
	const testVM = "tf-wsman-test-sweep"
	cleanup := func() {
		cs, err := c.FindComputerSystemByElementName(ctx, testVM)
		if err != nil {
			return // 不在
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

	var createdGUID string
	t.Run("write/DefineSystem(最小 Gen2)", func(t *testing.T) {
		// 最小設定 (ElementName + SubType) で base の embedded instance 形式を検証。
		sd := &Msvm_VirtualSystemSettingData{
			ElementName:          testVM,
			VirtualSystemSubType: VirtualSystemSubTypeGen2,
		}
		res, err := c.DefineSystem(ctx, sd)
		if err != nil {
			t.Errorf("FAIL: %v", err)
			var f *wsman.Fault
			if errors.As(err, &f) && f.Detail != "" {
				t.Logf("fault detail: %s", f.Detail)
			}
			return
		}
		if err := c.WaitForJob(ctx, res.JobRef); err != nil {
			t.Errorf("FAIL WaitForJob: %v", err)
			return
		}
		createdGUID = res.ResultingSystem
		t.Logf("created GUID=%s", createdGUID)
	})

	requireCreated := func(t *testing.T) {
		if createdGUID == "" {
			t.Skip("DefineSystem 失敗のためスキップ (依存)")
		}
	}

	t.Run("write/GetMemorySettings(created)", func(t *testing.T) {
		requireCreated(t)
		if _, err := c.GetMemorySettings(ctx, createdGUID); err != nil {
			t.Errorf("FAIL: %v", err)
		}
	})
	t.Run("write/SetMemorySettings", func(t *testing.T) {
		requireCreated(t)
		mem, err := c.GetMemorySettings(ctx, createdGUID)
		if err != nil {
			t.Skip("GetMemorySettings 失敗のためスキップ (依存)")
		}
		mem.VirtualQuantity = 1024
		jr, err := c.SetMemorySettings(ctx, mem)
		if err != nil {
			t.Errorf("FAIL: %v", err)
			return
		}
		if err := c.WaitForJob(ctx, jr); err != nil {
			t.Errorf("FAIL WaitForJob: %v", err)
		}
	})
	t.Run("write/SetProcessorSettings", func(t *testing.T) {
		requireCreated(t)
		proc, err := c.GetProcessorSettings(ctx, createdGUID)
		if err != nil {
			t.Skip("GetProcessorSettings 失敗のためスキップ (依存)")
		}
		proc.VirtualQuantity = 2
		jr, err := c.SetProcessorSettings(ctx, proc)
		if err != nil {
			t.Errorf("FAIL: %v", err)
			return
		}
		if err := c.WaitForJob(ctx, jr); err != nil {
			t.Errorf("FAIL WaitForJob: %v", err)
		}
	})
	t.Run("write/ModifySystemSettings(UpdateVm)", func(t *testing.T) {
		requireCreated(t)
		sd, err := c.GetSystemSettingData(ctx, createdGUID)
		if err != nil {
			t.Skip("GetSystemSettingData 失敗のためスキップ (依存)")
		}
		sd.Notes = []string{"sweep-update"}
		jr, err := c.UpdateVm(ctx, sd)
		if err != nil {
			t.Errorf("FAIL: %v", err)
			return
		}
		if err := c.WaitForJob(ctx, jr); err != nil {
			t.Errorf("FAIL WaitForJob: %v", err)
		}
	})
	t.Run("write/DestroySystem", func(t *testing.T) {
		requireCreated(t)
		jr, err := c.DestroySystem(ctx, createdGUID)
		if err != nil {
			t.Errorf("FAIL: %v", err)
			return
		}
		if err := c.WaitForJob(ctx, jr); err != nil {
			t.Errorf("FAIL WaitForJob: %v", err)
			return
		}
		createdGUID = "" // 破棄済み (cleanup の二重破棄を避ける)
	})
}
