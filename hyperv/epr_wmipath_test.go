package hyperv

import (
	"strings"
	"testing"
)

// wmiObjectPath の形式を固定する。
//
// 🔴 **この関数にはテストが 1 つも無かった** (#198)。形式は
// TestClient_CreateSwitch_* 等が完全一致で間接的に見ていたが、
// エスケープ規則とキー順はどのテストも直接固定していなかった。
//
// エスケープは実際に事故っている箇所: `InstanceID` は必ず `\` を含むので
// (`Microsoft:<GUID>\<index>` の形)、`\` → `\\` を落とすと実機が
// ErrorCode=32773 を返す (#114)。
func TestWmiObjectPath(t *testing.T) {
	const nicURI = nsVirtV2 + "/Msvm_ExternalEthernetPort"

	tests := []struct {
		name string
		uri  string
		keys map[string]string
		want string
	}{
		{
			name: "キー 1 つ",
			uri:  nicURI,
			keys: map[string]string{"DeviceID": "Microsoft:{guid}"},
			want: `root/virtualization/v2:Msvm_ExternalEthernetPort.DeviceID="Microsoft:{guid}"`,
		},
		{
			// 🔴 入力の map は順序を持たないので、**出力の安定化はこの関数の責務**。
			// 入力順に依存すると送信 XML が実行ごとに変わり、golden 比較が壊れる。
			name: "キーは名前昇順 (入力順に依存しない)",
			uri:  nicURI,
			keys: map[string]string{
				"SystemName":              "host-1",
				"CreationClassName":       "Msvm_ExternalEthernetPort",
				"DeviceID":                "Microsoft:{guid}",
				"SystemCreationClassName": "Msvm_ComputerSystem",
			},
			want: `root/virtualization/v2:Msvm_ExternalEthernetPort.` +
				`CreationClassName="Msvm_ExternalEthernetPort",` +
				`DeviceID="Microsoft:{guid}",` +
				`SystemCreationClassName="Msvm_ComputerSystem",` +
				`SystemName="host-1"`,
		},
		{
			// InstanceID は必ず `\` を含む形で来る (#114 で実機が 32773 を返した原因)。
			name: `値の \ は \\ になる`,
			uri:  nsVirtV2 + "/Msvm_ResourceAllocationSettingData",
			keys: map[string]string{"InstanceID": `Microsoft:{guid}\0`},
			want: `root/virtualization/v2:Msvm_ResourceAllocationSettingData.` +
				`InstanceID="Microsoft:{guid}\\0"`,
		},
		{
			name: `値の " は \" になる`,
			uri:  nicURI,
			keys: map[string]string{"DeviceID": `a"b`},
			want: `root/virtualization/v2:Msvm_ExternalEthernetPort.DeviceID="a\"b"`,
		},
		{
			// `\` を先に処理しないと `"` → `\"` の `\` が二重化される。
			name: `\ と " が両方ある場合の順序`,
			uri:  nicURI,
			keys: map[string]string{"DeviceID": `a\b"c`},
			want: `root/virtualization/v2:Msvm_ExternalEthernetPort.DeviceID="a\\b\"c"`,
		},
		{
			name: "キーが無ければ class までで終わる",
			uri:  nicURI,
			keys: nil,
			want: `root/virtualization/v2:Msvm_ExternalEthernetPort`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wmiObjectPath(tt.uri, tt.keys)
			if got != tt.want {
				t.Errorf("= %q\nwant %q", got, tt.want)
			}
			// 前置 \\HOST\ は付けない (#198 でデッドコードを除去した)。
			if strings.HasPrefix(got, `\\`) {
				t.Errorf("相対パスのはずが前置が付いている: %q", got)
			}
		})
	}
}

// TestWmiObjectPathIsDeterministic は map の反復順に依存しないことを明示的に見る。
//
// 上のテーブルは入力ごとに 1 回しか呼ばないので、**たまたま昇順で回ると通る**。
// Go の map 反復順はランダム化されているので、同じ入力を繰り返し呼んで
// 全部一致することを確かめる。
//
// 実測 (sort.Strings を除去した状態): テーブル単体は 60 回中 5 回 PASS (約 8%)、
// このテストは 20 回中 20 回 FAIL。**flaky に頼らず決定的に落とす**ためにこちらが要る。
func TestWmiObjectPathIsDeterministic(t *testing.T) {
	keys := map[string]string{
		"SystemName":              "host-1",
		"CreationClassName":       "Msvm_ExternalEthernetPort",
		"DeviceID":                "Microsoft:{guid}",
		"SystemCreationClassName": "Msvm_ComputerSystem",
	}
	first := wmiObjectPath(nsVirtV2+"/Msvm_ExternalEthernetPort", keys)
	for i := 0; i < 200; i++ {
		if got := wmiObjectPath(nsVirtV2+"/Msvm_ExternalEthernetPort", keys); got != first {
			t.Fatalf("%d 回目で結果が変わった:\n got  %q\n want %q", i+1, got, first)
		}
	}
}
