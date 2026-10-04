package hyperv

import (
	"context"
	"strings"
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
		if !strings.Contains(unescapeForAssert(bodies[0]), want) {
			t.Errorf("DefineSystem に VESMS の CreationClassName selector が無い。"+
				"実機は InternalError を返す (#145):\n%s", bodies[0])
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
		if !strings.Contains(unescapeForAssert(bodies[2]), want) {
			t.Errorf("DestroySystem に VESMS の CreationClassName selector が無い。"+
				"実機は InternalError を返す (#145):\n%s", bodies[2])
		}
	})
}
