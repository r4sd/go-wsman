//go:build integration

package hyperv

// ModifySystemSettings 経由の SecureBootTemplateId 書き込みが実際に反映されることの回帰テスト。
//
// 経緯: terraform-provider-hyperv #117 で SecureBootTemplate の GUID 表を拡張した際、CIM 経由の
// SecureBootTemplateId **書き込み**が初めて実トラフィックの流れる経路になった。当時の実機確認は
// 「PS で書いて CIM で読む」方向だけで、逆方向は未検証だった (Fable レビュー指摘)。もし
// Hyper-V がこのフィールドを黙って無視するなら「成功報告なのに実機は変わらない」= 黙殺になる。
// 2026-08-01 に実機で検証し、MicrosoftWindows → MicrosoftUEFICertificateAuthority への変更が
// 実際に反映されることを確認済み。SecureBootTemplateId は MOF 上 read-only と書かれているが
// 「ModifyVirtualSystem で変更できる」と MOF 自身が但し書きしており、その通りの挙動だった。
//
// 実行:
//
//	WSMAN_ENDPOINT=https://<hyperv-host>:5986/wsman WSMAN_USERNAME=<user> \
//	WSMAN_PASSWORD=... go test -tags=integration -v ./hyperv/ -run TestSecureBootTemplateWrite

import (
	"context"
	"strings"
	"testing"
)

const (
	sbTemplateMicrosoftWindows = "1734C6E8-3154-4DDA-BA5F-A874CC483422"
	sbTemplateUEFICA           = "272E7447-90A4-4563-A4B9-8E4AB00526CE"
)

func TestSecureBootTemplateWrite(t *testing.T) {
	c := sweepClient(t)
	ctx := context.Background()

	const testVM = "tf-wsman-test-sbtemplate"
	cleanup := func() {
		cs, err := c.FindComputerSystemByElementName(ctx, testVM)
		if err != nil {
			return
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
	cleanup()

	res, err := c.DefineSystem(ctx, &Msvm_VirtualSystemSettingData{
		ElementName:          testVM,
		VirtualSystemSubType: VirtualSystemSubTypeGen2,
	})
	if err != nil {
		t.Fatalf("DefineSystem: %v", err)
	}
	if err := c.WaitForJob(ctx, res.JobRef); err != nil {
		t.Fatalf("WaitForJob(DefineSystem): %v", err)
	}
	guid := res.ResultingSystem

	before, err := c.GetSystemSettingData(ctx, guid)
	if err != nil {
		t.Fatalf("GetSystemSettingData (before): %v", err)
	}
	t.Logf("BEFORE SecureBoot=%v SecureBootTemplateId=%q", before.SecureBoot, before.SecureBootTemplateId)

	if !strings.EqualFold(before.SecureBootTemplateId, sbTemplateMicrosoftWindows) {
		t.Logf("⚠️ 既定テンプレートが MicrosoftWindows ではない。以降の判定は参考値")
	}

	// 本題: ModifySystemSettings で SecureBootTemplateId を UEFI CA (Linux 用) に変更する。
	// provider の applyFirmwareSettings → UpdateVm と同じ経路 (最小インスタンス送信)。
	jobRef, err := c.UpdateVm(ctx, &Msvm_VirtualSystemSettingData{
		InstanceID:           before.InstanceID,
		SecureBoot:           true,
		SecureBootTemplateId: sbTemplateUEFICA,
	})
	if err != nil {
		logFaultDetail(t, err)
		t.Fatalf("🔴 判定: SecureBootTemplateId の書き込みが拒否された: %v", err)
	}
	if err := c.WaitForJob(ctx, jobRef); err != nil {
		t.Fatalf("🔴 判定: 呼び出しは通ったが Job が失敗: %v", err)
	}

	// 成功報告を信用せず読み直す。
	after, err := c.GetSystemSettingData(ctx, guid)
	if err != nil {
		t.Fatalf("GetSystemSettingData (after): %v", err)
	}
	t.Logf("AFTER  SecureBoot=%v SecureBootTemplateId=%q", after.SecureBoot, after.SecureBootTemplateId)

	if strings.EqualFold(after.SecureBootTemplateId, sbTemplateUEFICA) {
		t.Logf("🎯 判定: CIM 経由の SecureBootTemplateId 書き込みは実際に反映される (安全)")
		return
	}
	if strings.EqualFold(after.SecureBootTemplateId, before.SecureBootTemplateId) {
		t.Fatalf("🔴 判定: 黙殺。Job は成功したのにテンプレートが変わっていない (got %q, want %q)",
			after.SecureBootTemplateId, sbTemplateUEFICA)
	}
	t.Fatalf("🔴 判定: 想定外の値になった: got %q", after.SecureBootTemplateId)
}
