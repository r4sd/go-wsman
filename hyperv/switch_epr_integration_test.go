//go:build integration

package hyperv

import (
	"context"
	"testing"

	"github.com/r4sd/go-wsman/wsman"
)

// TestRealSwitchEPRSelectors は MOF 準拠の 2 Selector (CreationClassName + Name) が
// スイッチを一意に解決できることを実機で確認する (#134)。
//
// 修正前は存在しない SystemCreationClassName / SystemName を足していた。
// 単体テストは「送っていないこと」しか見られないので、実機では
// 「減らした Selector でも実インスタンスに解決できる」ことを押さえる。
//
// 非破壊 (Get のみ)。既存スイッチを 1 つ拾って読むだけで、作成も削除もしない。
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
	t.Logf("🎯 判定: CreationClassName + Name の 2 つでスイッチを一意に解決できる")
}
