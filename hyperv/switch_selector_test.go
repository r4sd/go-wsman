package hyperv

import (
	"context"
	"testing"
)

// TestSwitchManagementServiceSelectors は VESMS のメソッド呼び出しに
// CreationClassName selector が付くことを検証する (#145)。
//
// 🔴 これが無いと実機は WS-Man Fault [s:Receiver/w:InternalError] を返す。
// 2026-10-05 実機で確認:
//
//	selector 無し          → InternalError
//	CreationClassName 付き → ReturnValue=0 でスイッチが作られる
//
// VSMS が同じ要求を持つこと (vsmsSelectors) は既知だったが、
// スイッチ管理サービス (VESMS) にも同じ要求があることは見落とされていた。
// Hyper-V WMI プロバイダ (WsmWmiPl.dll) はメソッド実行時にインスタンスを特定する
// selector を要求する。
//
// ヘッダの SelectorSet を見る。EPR 内の SelectorSet
// (TestClient_DestroySwitch_SelectorSet が見ているもの) とは別物。
//
// Contains ではなく **SelectorSet 全体を厳密比較**する。Contains だと余計な
// Selector (SystemName 等) を足されても通ってしまう。ヘッダ側は整形されて改行が
// 入るので collapseTagWhitespace を通す。
func TestSwitchManagementServiceSelectors(t *testing.T) {
	const want = `<w:Selector Name="CreationClassName">Msvm_VirtualEthernetSwitchManagementService</w:Selector>`

	t.Run("CreateSwitch", func(t *testing.T) {
		resp := loadGolden(t, "invoke_response_define_switch.xml")
		var bodies []string
		server := newSequenceServer(t, []string{resp}, &bodies)
		defer server.Close()

		client, _ := NewClient(server.URL)
		if _, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{
			Name: "sw1", Type: SwitchTypePrivate,
		}); err != nil {
			t.Fatalf("CreateSwitch: %v", err)
		}
		got := collapseTagWhitespace(selectorSetInner(t, unescapeForAssert(bodies[0])))
		if got != want {
			t.Errorf("DefineSystem のヘッダ SelectorSet が一致しない。"+
				"実機は selector 不足で InternalError を返す (#145)\n got:  %s\n want: %s", got, want)
		}
	})

	t.Run("DestroySwitch", func(t *testing.T) {
		enum := loadGolden(t, "enumerate_response_virtualethernetswitch.xml")
		pull := loadGolden(t, "pull_response_virtualethernetswitch.xml")
		resp := loadGolden(t, "invoke_response_destroy_switch.xml")
		var bodies []string
		server := newSequenceServer(t, []string{enum, pull, resp}, &bodies)
		defer server.Close()

		client, _ := NewClient(server.URL)
		if _, err := client.DestroySwitch(context.Background(), "Internal"); err != nil {
			t.Fatalf("DestroySwitch: %v", err)
		}
		got := collapseTagWhitespace(selectorSetInner(t, unescapeForAssert(bodies[2])))
		if got != want {
			t.Errorf("DestroySystem のヘッダ SelectorSet が一致しない。"+
				"実機は selector 不足で InternalError を返す (#145)\n got:  %s\n want: %s", got, want)
		}
	})
}
