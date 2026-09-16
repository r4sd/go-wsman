package hyperv

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// enumResponseGeneric は EnumerationContext だけを返す汎用の EnumerateResponse。
const enumResponseGeneric = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:e="http://schemas.xmlsoap.org/ws/2004/09/enumeration">
  <s:Header><a:Action>http://schemas.xmlsoap.org/ws/2004/09/enumeration/EnumerateResponse</a:Action></s:Header>
  <s:Body><e:EnumerateResponse><e:EnumerationContext>uuid:ctx</e:EnumerationContext></e:EnumerateResponse></s:Body>
</s:Envelope>`

// compInstance は Component SettingData の 1 インスタンス分の XML を組み立てる。
func compInstance(class, instanceID, elementName string, enabledState uint16) string {
	return fmt.Sprintf(`        <p:%s>
          <p:InstanceID>%s</p:InstanceID>
          <p:ElementName>%s</p:ElementName>
          <p:EnabledState>%d</p:EnabledState>
        </p:%s>`, class, instanceID, elementName, enabledState, class)
}

// compPull は指定クラスの PullResponse を組み立てる (0 件以上のインスタンス)。
func compPull(class string, items ...string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:e="http://schemas.xmlsoap.org/ws/2004/09/enumeration" xmlns:p="http://schemas.microsoft.com/wbem/wsman/1/wmi/root/virtualization/v2/%s">
  <s:Header><a:Action>http://schemas.xmlsoap.org/ws/2004/09/enumeration/PullResponse</a:Action></s:Header>
  <s:Body><e:PullResponse><e:Items>
%s
      </e:Items><e:EndOfSequence/></e:PullResponse></s:Body>
</s:Envelope>`, class, strings.Join(items, "\n"))
}

// integrationClassOrder は integrationComponentEnumOrder に対応する CIM クラス名の順序。
var integrationClassOrder = []string{
	"Msvm_HeartbeatComponentSettingData",
	"Msvm_KvpExchangeComponentSettingData",
	"Msvm_ShutdownComponentSettingData",
	"Msvm_TimeSyncComponentSettingData",
	"Msvm_VssComponentSettingData",
	"Msvm_GuestServiceInterfaceComponentSettingData",
}

// integrationSequence は 6 クラス分の [enum, pull] 応答列を integrationClassOrder の順で組み立てる。
func integrationSequence(pulls map[string]string) []string {
	seq := make([]string, 0, len(integrationClassOrder)*2)
	for _, cls := range integrationClassOrder {
		seq = append(seq, enumResponseGeneric, pulls[cls])
	}
	return seq
}

// assertEnumeratedClassOrder は i 番目の Enumerate が integrationClassOrder[i] のクラス宛てで
// あることを確かめる。
//
// 応答列サーバは呼び出し回数だけで応答を返し、リクエストの ResourceURI を見ない。そのため
// これが無いと「Component は列挙順の i 番目」「応答は列挙順の i 番目」を突き合わせるだけの
// トートロジーになり、integrationComponentByName の URI を 2 つ入れ替えても全テストが緑になる。
// Component をループ側から取るようになった以降、この取り違えは Read と Write の両方が
// 一貫して別のコンポーネントを指す silent corruption になるため、ここで縛る。
func assertEnumeratedClassOrder(t *testing.T, bodies []string) {
	t.Helper()
	for i, cls := range integrationClassOrder {
		body := bodies[i*2] // [enum, pull] の enum 側
		if !strings.Contains(body, "/"+cls) {
			t.Errorf("bodies[%d]: %s 宛ての Enumerate でない: %s", i*2, cls, body)
		}
	}
}

// TestClient_ListIntegrationServices は 6 クラスを列挙して表示名と有効状態を写し、
// EnabledState 2/3 を Enabled true/false に正しく変換すること、別 VM の割当を除外すること、
// 返り値が Name 昇順で安定することを検証する。
func TestClient_ListIntegrationServices(t *testing.T) {
	const vm = "11111111-aaaa-bbbb-cccc-000000000001"
	const other = "22222222-aaaa-bbbb-cccc-000000000002"
	id := func(guid, res string) string { return "Microsoft:" + guid + "\\" + res }

	pulls := map[string]string{
		"Msvm_HeartbeatComponentSettingData": compPull("Msvm_HeartbeatComponentSettingData",
			compInstance("Msvm_HeartbeatComponentSettingData", id(vm, "hb"), "Heartbeat", EnabledStateEnabled)),
		"Msvm_KvpExchangeComponentSettingData": compPull("Msvm_KvpExchangeComponentSettingData",
			compInstance("Msvm_KvpExchangeComponentSettingData", id(vm, "kvp"), "Key-Value Pair Exchange", EnabledStateEnabled)),
		"Msvm_ShutdownComponentSettingData": compPull("Msvm_ShutdownComponentSettingData",
			compInstance("Msvm_ShutdownComponentSettingData", id(vm, "sd"), "Shutdown", EnabledStateEnabled)),
		"Msvm_TimeSyncComponentSettingData": compPull("Msvm_TimeSyncComponentSettingData",
			compInstance("Msvm_TimeSyncComponentSettingData", id(vm, "ts"), "Time Synchronization", EnabledStateEnabled)),
		"Msvm_VssComponentSettingData": compPull("Msvm_VssComponentSettingData",
			compInstance("Msvm_VssComponentSettingData", id(vm, "vss"), "VSS", EnabledStateEnabled)),
		// Guest Service Interface は既定で無効 (EnabledState=3)。別 VM の割当も混ぜて除外を確認する。
		"Msvm_GuestServiceInterfaceComponentSettingData": compPull("Msvm_GuestServiceInterfaceComponentSettingData",
			compInstance("Msvm_GuestServiceInterfaceComponentSettingData", id(other, "gsi"), "Guest Service Interface", EnabledStateEnabled),
			compInstance("Msvm_GuestServiceInterfaceComponentSettingData", id(vm, "gsi"), "Guest Service Interface", EnabledStateDisabled)),
	}

	var bodies []string
	server := newSequenceServer(t, integrationSequence(pulls), &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListIntegrationServices(context.Background(), vm)
	if err != nil {
		t.Fatalf("ListIntegrationServices: %v", err)
	}

	// 別 VM の Guest Service Interface を除外し、対象 VM の 6 サービスだけ。
	want := map[string]bool{
		"Guest Service Interface": false, // EnabledState=3
		"Heartbeat":               true,
		"Key-Value Pair Exchange": true,
		"Shutdown":                true,
		"Time Synchronization":    true,
		"VSS":                     true,
	}
	if len(got) != len(want) {
		t.Fatalf("len: got %d, want %d (%v)", len(got), len(want), got)
	}
	// Name 昇順で安定していること。
	prev := ""
	for _, svc := range got {
		if prev != "" && svc.Name < prev {
			t.Errorf("結果が Name 昇順でない: %q < %q", svc.Name, prev)
		}
		prev = svc.Name
		wantEnabled, ok := want[svc.Name]
		if !ok {
			t.Errorf("想定外のサービス %q", svc.Name)
			continue
		}
		if svc.Enabled != wantEnabled {
			t.Errorf("%q Enabled: got %v, want %v", svc.Name, svc.Enabled, wantEnabled)
		}
	}

	assertEnumeratedClassOrder(t, bodies)

	// Hyper-V は WQL フィルタ列挙を拒否するため、全 6 クラスの Enumerate を無フィルタで送ること (#80)。
	for i, b := range bodies {
		if strings.Contains(b, "Filter") || strings.Contains(b, "SELECT") {
			t.Errorf("enumerate should be unfiltered (no WQL Filter); bodies[%d]: %s", i, b)
		}
	}
}

// TestClient_ListIntegrationServices_Empty は対象 VM に割当が無い (別 VM のみ) 場合に
// 空スライス・no-error を返すことを検証する。
func TestClient_ListIntegrationServices_Empty(t *testing.T) {
	const other = "22222222-aaaa-bbbb-cccc-000000000002"
	id := "Microsoft:" + other + "\\hb"
	pulls := map[string]string{
		"Msvm_HeartbeatComponentSettingData": compPull("Msvm_HeartbeatComponentSettingData",
			compInstance("Msvm_HeartbeatComponentSettingData", id, "Heartbeat", EnabledStateEnabled)),
		"Msvm_KvpExchangeComponentSettingData":           compPull("Msvm_KvpExchangeComponentSettingData"),
		"Msvm_ShutdownComponentSettingData":              compPull("Msvm_ShutdownComponentSettingData"),
		"Msvm_TimeSyncComponentSettingData":              compPull("Msvm_TimeSyncComponentSettingData"),
		"Msvm_VssComponentSettingData":                   compPull("Msvm_VssComponentSettingData"),
		"Msvm_GuestServiceInterfaceComponentSettingData": compPull("Msvm_GuestServiceInterfaceComponentSettingData"),
	}
	var bodies []string
	server := newSequenceServer(t, integrationSequence(pulls), &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListIntegrationServices(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("ListIntegrationServices: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len: got %d, want 0 (対象 VM に割当なし)", len(got))
	}
}

// TestClient_ListIntegrationServices_EmptyVMGUID は空 vmGUID を弾くことを検証する。
func TestClient_ListIntegrationServices_EmptyVMGUID(t *testing.T) {
	client, _ := NewClient("https://example.invalid:5986/wsman")
	if _, err := client.ListIntegrationServices(context.Background(), ""); err == nil {
		t.Fatal("空 vmGUID はエラーになるべき")
	}
}

// TestClient_ListIntegrationServices_LocalizedElementName は ElementName がホスト OS 言語に
// ローカライズされていても、Component が列挙した CIM クラスから正しく決まることを検証する (#161)。
// Name (表示名) は実機が返したまま、Component は英語固定の識別子になる。
func TestClient_ListIntegrationServices_LocalizedElementName(t *testing.T) {
	const vm = "11111111-aaaa-bbbb-cccc-000000000001"
	id := func(res string) string { return "Microsoft:" + vm + "\\" + res }

	// ElementName がローカライズされていても Component が CIM クラス側から決まることを見る。
	//
	// 🔴 実測値は "キー値ペア交換" の 1 件だけ (terraform-provider-hyperv #98 の再現ログ。
	// 日本語ホストで unknown component "キー値ペア交換" が出た)。残り 5 件は**実機の値ではなく
	// 合成文字列**で、日本語ホストがこう返すと主張するものではない。
	// このテストの主張は「Component が ElementName に依存しない」ことなので、英語の正規名と
	// 一致しない文字列でありさえすればよく、実機の翻訳を当てる必要がない。
	// 実測していない翻訳を書くと、それが実機の挙動として固定されてしまう。
	localized := map[string]string{
		"Msvm_KvpExchangeComponentSettingData":           "キー値ペア交換", // 実測 (provider #98)
		"Msvm_HeartbeatComponentSettingData":             "[合成] Heartbeat のローカライズ名",
		"Msvm_ShutdownComponentSettingData":              "[合成] Shutdown のローカライズ名",
		"Msvm_TimeSyncComponentSettingData":              "[合成] TimeSync のローカライズ名",
		"Msvm_VssComponentSettingData":                   "[合成] Vss のローカライズ名",
		"Msvm_GuestServiceInterfaceComponentSettingData": "[合成] GuestServiceInterface のローカライズ名",
	}
	pulls := make(map[string]string, len(localized))
	for cls, name := range localized {
		pulls[cls] = compPull(cls, compInstance(cls, id(cls), name, EnabledStateEnabled))
	}

	var bodies []string
	server := newSequenceServer(t, integrationSequence(pulls), &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListIntegrationServices(context.Background(), vm)
	if err != nil {
		t.Fatalf("ListIntegrationServices: %v", err)
	}
	if len(got) != len(localized) {
		t.Fatalf("len: got %d, want %d (%v)", len(got), len(localized), got)
	}

	wantComponent := map[IntegrationServiceComponent]string{
		IntegrationServiceHeartbeat:             localized["Msvm_HeartbeatComponentSettingData"],
		IntegrationServiceKeyValuePairExchange:  localized["Msvm_KvpExchangeComponentSettingData"],
		IntegrationServiceShutdown:              localized["Msvm_ShutdownComponentSettingData"],
		IntegrationServiceTimeSynchronization:   localized["Msvm_TimeSyncComponentSettingData"],
		IntegrationServiceVSS:                   localized["Msvm_VssComponentSettingData"],
		IntegrationServiceGuestServiceInterface: localized["Msvm_GuestServiceInterfaceComponentSettingData"],
	}
	var prev IntegrationServiceComponent
	for _, svc := range got {
		wantName, ok := wantComponent[svc.Component]
		if !ok {
			t.Errorf("想定外の Component %q (Name=%q)", svc.Component, svc.Name)
			continue
		}
		if svc.Name != wantName {
			t.Errorf("%q Name: got %q, want %q (ElementName は実機の値のまま残す)", svc.Component, svc.Name, wantName)
		}
		// ローカライズされた Name ではなく Component の昇順で安定させる。
		if prev != "" && svc.Component < prev {
			t.Errorf("結果が Component 昇順でない: %q < %q", svc.Component, prev)
		}
		prev = svc.Component
	}

	// Component が「i 番目に列挙したクラス」と結び付いていることの本体。
	assertEnumeratedClassOrder(t, bodies)
}
