//go:build integration

package hyperv

import (
	"context"
	"testing"

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
// 本来は使い捨てスイッチで create → destroy したかったが、CreateSwitch 自体が
// 実機で InternalError になる (#145) ため代替している。
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
