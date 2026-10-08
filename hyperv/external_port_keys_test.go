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

// parseWMIObjectPathKeys は WMI オブジェクトパスの `Key="value"` 群を Selector に変換する。
//
//	root/virtualization/v2:Msvm_ExternalEthernetPort.CreationClassName="...",DeviceID="..."
//
// (実装の wmiObjectPath は host 引数が空なので前置の \\<HOST>\ は付かない)
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

// TestExternalAdapterBindingKeys は実装が組む HostResource のキーを**タグ無しで**固定する。
//
// 🔴 **統合テスト側のヘルパがここで守られる。** 上のパーサ群は以前
// `//go:build integration` 配下にあり、通常の `go test ./...` では
// コンパイルすらされなかった。壊れても次に実機でテストを回すまで気付かない。
//
// 役割分担:
//
//	パス文字列の**構文** (区切り・引用・前置) → TestClient_CreateSwitch_External (完全一致)
//	**キーの選び方**とパーサ                  → このテスト
//	キーが実機でその NIC を解決すること       → TestIntegration_ExternalAdapterBindingKeysResolve
func TestExternalAdapterBindingKeys(t *testing.T) {
	// CreateSwitch_External と同じ列挙シーケンス (enum + 実機記録 + 終端の legacy pull)。
	responses := []string{
		loadGolden(t, "enumerate_response_externalethernetport.xml"),
		loadGolden(t, "recorded_pull_externalethernetport.xml"),
		loadGolden(t, "pull_response_externalethernetport.xml"),
	}
	var bodies []string
	server := newSequenceServer(t, responses, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	embedded, err := client.buildExternalAdapterBinding(context.Background(), CreateSwitchOptions{
		Name:            "ExternalSwitch",
		ExternalAdapter: "Intel(R) Ethernet Connection (2) I218-LM",
	})
	if err != nil {
		t.Fatalf("buildExternalAdapterBinding: %v", err)
	}

	m := hostResourcePattern.FindStringSubmatch(embedded)
	if m == nil {
		t.Fatalf("HostResource を取り出せない:\n%s", embedded)
	}
	selectors := parseWMIObjectPathKeys(t, m[1])

	// キー名が MOF の 4 つであること。Name は**キーではない**ので入ってはいけない。
	wantKeys := map[string]string{
		"CreationClassName":       "Msvm_ExternalEthernetPort",
		"DeviceID":                "Microsoft:{00000000-0000-4000-8000-000000000007}",
		"SystemCreationClassName": "Msvm_ComputerSystem",
		"SystemName":              "scrubbed-1",
	}
	got := make(map[string]string, len(selectors))
	for _, s := range selectors {
		got[s.Name] = s.Value
	}
	if len(got) != len(wantKeys) {
		t.Errorf("キーが %d 個 (want %d): %v", len(got), len(wantKeys), selectorNames(selectors))
	}
	for k, want := range wantKeys {
		v, ok := got[k]
		if !ok {
			t.Errorf("キー %s が無い: %v", k, selectorNames(selectors))
			continue
		}
		// **値まで見る。** キー名が揃っていても値を取り違えていたら実機で解決しない。
		if v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
	if _, bad := got["Name"]; bad {
		t.Error("Name がキーに入っている。Name は CIM_ManagedSystemElement 由来で**キーではない** (#146)")
	}
}
