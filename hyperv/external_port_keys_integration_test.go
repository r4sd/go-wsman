//go:build integration

package hyperv

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/r4sd/go-wsman/wsman"
)

// hostResourcePattern は embedded instance の HostResource に入っている WMI オブジェクトパスを取り出す。
var hostResourcePattern = regexp.MustCompile(
	`(?s)<PROPERTY\.ARRAY NAME="HostResource".*?<VALUE>([^<]*)</VALUE>`)

// TestIntegration_ExternalAdapterBindingKeysResolve は #146 を実機で検証する。
//
// # なぜこの形で検証するか
//
// #146 は「`Msvm_ExternalEthernetPort` の参照が MOF のキーと一致していない」という話で、
// 以前は `CreationClassName` + `Name` を送っていた (`Name` はキーではない)。
//
// 🔴 **スイッチの作成成否では判定できない。** 検証環境の物理 NIC は既存スイッチに
// 束ねられており (`IsBound=true`)、束ねられた NIC への再バインドは
// **キーが正しくても間違っていても `ErrorCode=32773`** になる。
//
// そこで「作れるか」ではなく **「組み立てた参照がその NIC を指すか」** を直接見る。
// これは Get で読み取るだけなので未束縛の NIC が要らない。
//
// # 何を担保するか / しないか
//
//	担保する:   buildExternalAdapterBinding が組む HostResource のキーが、
//	            実機でその NIC を一意に解決する
//	担保しない: External スイッチが実際に作成できること・通信できること。
//	            そこは未束縛の物理 NIC が使える環境が必要 (#146 に残す)
func TestIntegration_ExternalAdapterBindingKeysResolve(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	ports, err := client.ListExternalEthernetPorts(ctx)
	if err != nil {
		t.Fatalf("ListExternalEthernetPorts: %v", err)
	}
	if len(ports) == 0 {
		t.Skip("ホストに Msvm_ExternalEthernetPort が無い")
	}

	var target *Msvm_ExternalEthernetPort
	for _, p := range ports {
		if p.ElementName != "" && p.DeviceID != "" && p.SystemName != "" {
			target = p
			break
		}
	}
	if target == nil {
		t.Fatalf("MOF キーが揃ったポートが無い (%d 件列挙)", len(ports))
	}
	t.Logf("対象ポート: ElementName=%q IsBound=%v", target.ElementName, target.IsBound)

	// --- 実装が組み立てた参照を取り出す ---
	embedded, err := client.buildExternalAdapterBinding(ctx, CreateSwitchOptions{
		Name:            "go-wsman-acctest-146-not-created",
		ExternalAdapter: target.ElementName,
	})
	if err != nil {
		t.Fatalf("buildExternalAdapterBinding: %v", err)
	}
	m := hostResourcePattern.FindStringSubmatch(embedded)
	if m == nil {
		t.Fatalf("HostResource を取り出せない:\n%s", embedded)
	}
	selectors := parseWMIObjectPathKeys(t, m[1])
	t.Logf("組み立てられたキー: %v", selectorNames(selectors))

	// --- 本題: そのキーで実機の NIC が解決するか ---
	inst, err := client.wsman.Get(ctx, msvmExternalEthernetPortURI, selectors...)
	if err != nil {
		t.Fatalf("組み立てたキーで Get できない (#146 のキー不一致が残っている): %v", err)
	}
	if got := inst.Property("DeviceID"); got != target.DeviceID {
		t.Errorf("解決したのは別のポート: DeviceID = %q, want %q", got, target.DeviceID)
	}
	if got := inst.Property("ElementName"); got != target.ElementName {
		t.Errorf("解決したのは別のポート: ElementName = %q, want %q", got, target.ElementName)
	}

	// --- 陰性対照: 旧実装のキーでは解決しないこと ---
	//
	// これが無いと「どんなキーでも解決する」のか「正しいキーだから解決した」のかが
	// 区別できない。旧形式が通ってしまうなら、この検査には #146 を落とす力が無い。
	if _, err := client.wsman.Get(ctx, msvmExternalEthernetPortURI,
		wsman.Selector{Name: "CreationClassName", Value: "Msvm_ExternalEthernetPort"},
		wsman.Selector{Name: "Name", Value: target.ElementName},
	); err == nil {
		t.Errorf("旧実装のキー (CreationClassName + Name) でも解決してしまった。" +
			"この検査には #146 を検出する力が無い")
	} else {
		t.Logf("陰性対照 OK: 旧キーは失敗する (%v)", err)
	}
}

// parseWMIObjectPathKeys は WMI オブジェクトパスの `Key="value"` 群を Selector に変換する。
//
//	\\<HOST>\root\virtualization\v2:Msvm_ExternalEthernetPort.CreationClassName="...",DeviceID="..."
func parseWMIObjectPathKeys(t *testing.T, path string) []wsman.Selector {
	t.Helper()
	i := strings.Index(path, ".")
	if i < 0 {
		t.Fatalf("WMI オブジェクトパスにキー部分が無い: %q", path)
	}
	// 🔴 **HostResource は XML テキストなので `"` が `&#34;` で乗っている。**
	// 先に実体参照を戻さないとキーが 1 つも取れない (Go の marshal が必ずこうする)。
	keys := xmlUnescapeForTest(path[i:])

	var out []wsman.Selector
	re := regexp.MustCompile(`([A-Za-z]+)="((?:[^"\\]|\\.)*)"`)
	for _, m := range re.FindAllStringSubmatch(keys, -1) {
		// WMI パスのエスケープを戻す (`\\` → `\`、`\"` → `"`)。
		v := strings.ReplaceAll(m[2], `\"`, `"`)
		v = strings.ReplaceAll(v, `\\`, `\`)
		out = append(out, wsman.Selector{Name: m[1], Value: v})
	}
	if len(out) == 0 {
		t.Fatalf("キーを 1 つも取り出せない: %q", path)
	}
	return out
}

func selectorNames(ss []wsman.Selector) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.Name)
	}
	return out
}

// xmlUnescapeForTest は XML の実体参照を戻す。
// embedded instance は XML テキストなので `"` が `&#34;` 等になっている。
func xmlUnescapeForTest(s string) string {
	for _, p := range [][2]string{
		{"&#34;", `"`}, {"&quot;", `"`},
		{"&#39;", "'"}, {"&apos;", "'"},
		{"&lt;", "<"}, {"&gt;", ">"},
		{"&amp;", "&"}, // 最後 (他の実体参照を壊さないため)
	} {
		s = strings.ReplaceAll(s, p[0], p[1])
	}
	return s
}
