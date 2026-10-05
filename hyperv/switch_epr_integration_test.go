//go:build integration

package hyperv

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/r4sd/go-wsman/wsman"
)

// TestRealSwitchEPRSelectors は MOF 準拠の 2 Selector (CreationClassName + Name) が
// 実機で拒否されないことを確認する (#134)。
//
// ⚠️ これは「DestroySystem / AddResourceSettings のパラメータとして通る」証明ではない。
// WS-Man の Get と、REF パラメータ / string プロパティとしての解決は経路が別で、
// WinRM は部分キーでも解決することがある。ここで押さえているのは
// 「この Selector 集合が実インスタンスに解決でき、値も一致する」ところまで。
//
// 非破壊 (Get のみ)。既存スイッチを 1 つ拾って読むだけで、作成も削除もしない。
//
// create → destroy の往復は TestIntegration_CreateDestroySwitchRoundTrip が見る (#177)。
// こちらは「既存スイッチに対して 2 Selector が解決できる」ことに絞って残す
// (作った直後のスイッチだけで通っても、PS / Hyper-V マネージャーが作った既存スイッチで
// 通るとは限らない。両方要る)。
func TestRealSwitchEPRSelectors(t *testing.T) {
	c := getIntegrationClient(t)
	ctx := context.Background()

	switches, err := c.ListVirtualEthernetSwitches(ctx)
	if err != nil {
		t.Fatalf("ListVirtualEthernetSwitches: %v", err)
	}
	if len(switches) == 0 {
		t.Skip("ホストに仮想スイッチが無い")
	}
	sw := switches[0]
	t.Logf("対象: ElementName=%q Name=%s", sw.ElementName, sw.Name)

	// MOF 準拠の 2 つだけで解決できること。
	resp, err := c.wsman.Get(ctx, msvmVirtualEthernetSwitchURI,
		wsman.Selector{Name: "CreationClassName", Value: "Msvm_VirtualEthernetSwitch"},
		wsman.Selector{Name: "Name", Value: sw.Name},
	)
	if err != nil {
		t.Fatalf("🔴 2 Selector (CreationClassName + Name) で解決できない: %v", err)
	}
	if got := resp.Property("Name"); got != sw.Name {
		t.Errorf("解決先が違う: Name=%q, want %q", got, sw.Name)
	}
	t.Logf("🎯 判定: CreationClassName + Name の 2 つが実機で拒否されず、同じインスタンスに解決される")
}

// TestIntegration_CreateDestroySwitchRoundTrip は使い捨てスイッチの
// create → 確認 → destroy → 残存ゼロ確認を実機で通す (#177 項目 1)。
//
// #145 (VESMS の selector 不足) が解消するまで書けなかったテスト。
//
// **手書き golden が固定していた形が実機と違っていた** (#177 項目 2):
//
//	                        手書き golden   実機
//	DefineSystem の         4096            0
//	  ReturnValue           (非同期 Job)    (同期完了)
//
// このテストを WSMAN_RECORD_DIR 付きで回すと、その実機の応答がそのまま記録される。
//
// 作ったスイッチは必ず消す。既存スイッチには触らない。
func TestIntegration_CreateDestroySwitchRoundTrip(t *testing.T) {
	if os.Getenv("HYPERV_TEST_ALLOW_MUTATION") == "" {
		t.Skip("HYPERV_TEST_ALLOW_MUTATION 未設定（スイッチ作成を伴う破壊的テスト）")
	}
	c := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 衝突しない名前にする。既存スイッチと取り違えて消さないよう接頭辞を付ける。
	name := fmt.Sprintf("gowsman-test-%d", time.Now().UnixNano())

	before, err := c.ListVirtualEthernetSwitches(ctx)
	if err != nil {
		t.Fatalf("ListVirtualEthernetSwitches (前提): %v", err)
	}

	// 後片付けは必ず走らせる。作成が途中で失敗しても残骸を残さない。
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		switches, err := c.ListVirtualEthernetSwitches(cleanupCtx)
		if err != nil {
			t.Errorf("後片付けの一覧取得に失敗: %v", err)
			return
		}
		for _, sw := range switches {
			if sw.ElementName != name {
				continue
			}
			if _, err := c.DestroySwitch(cleanupCtx, name); err != nil {
				t.Errorf("🔴 後片付けに失敗 (スイッチ %q が残っている): %v", name, err)
			}
		}
	})

	// 1. Private スイッチを作る (ResourceSettings なし = 最小構成)
	result, err := c.CreateSwitch(ctx, CreateSwitchOptions{
		Name: name,
		Type: SwitchTypePrivate,
	})
	if err != nil {
		t.Fatalf("CreateSwitch: %v", err)
	}
	t.Logf("CreateSwitch: SwitchRef=%q JobRef=%q", result.SwitchRef, result.JobRef)
	if result.JobRef != "" {
		if err := c.WaitForJob(ctx, result.JobRef); err != nil {
			t.Fatalf("CreateSwitch の Job 待ち: %v", err)
		}
	}

	// 2. 一覧に現れること
	after, err := c.ListVirtualEthernetSwitches(ctx)
	if err != nil {
		t.Fatalf("ListVirtualEthernetSwitches (作成後): %v", err)
	}
	if len(after) != len(before)+1 {
		t.Errorf("スイッチ数が %d → %d (want %d)", len(before), len(after), len(before)+1)
	}
	var created *Msvm_VirtualEthernetSwitch
	for _, sw := range after {
		if sw.ElementName == name {
			created = sw
			break
		}
	}
	if created == nil {
		t.Fatalf("作成したスイッチ %q が一覧に無い", name)
	}
	t.Logf("作成されたスイッチ: Name=%s", created.Name)

	// 3. GetVirtualEthernetSwitch で引けること (表示名 → GUID 解決)
	got, err := c.GetVirtualEthernetSwitch(ctx, name)
	if err != nil {
		t.Fatalf("GetVirtualEthernetSwitch: %v", err)
	}
	if got.Name != created.Name {
		t.Errorf("GetVirtualEthernetSwitch の Name が %q (want %q)", got.Name, created.Name)
	}

	// 4. 消せること
	jobRef, err := c.DestroySwitch(ctx, name)
	if err != nil {
		t.Fatalf("DestroySwitch: %v", err)
	}
	t.Logf("DestroySwitch: JobRef=%q", jobRef)
	if jobRef != "" {
		if err := c.WaitForJob(ctx, jobRef); err != nil {
			t.Fatalf("DestroySwitch の Job 待ち: %v", err)
		}
	}

	// 5. 残存ゼロ (件数が戻り、名前も消えている)
	final, err := c.ListVirtualEthernetSwitches(ctx)
	if err != nil {
		t.Fatalf("ListVirtualEthernetSwitches (削除後): %v", err)
	}
	if len(final) != len(before) {
		t.Errorf("削除後のスイッチ数が %d (want %d)", len(final), len(before))
	}
	for _, sw := range final {
		if sw.ElementName == name {
			t.Errorf("🔴 削除したスイッチ %q が残っている", name)
		}
	}
}
