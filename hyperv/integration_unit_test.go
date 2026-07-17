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

// integrationSequence は 6 クラス分の [enum, pull] 応答列を、integrationComponentURIs と
// 同じ順序 (Heartbeat, KVP, Shutdown, TimeSync, VSS, GuestService) で組み立てる。
func integrationSequence(pulls map[string]string) []string {
	classes := []string{
		"Msvm_HeartbeatComponentSettingData",
		"Msvm_KvpExchangeComponentSettingData",
		"Msvm_ShutdownComponentSettingData",
		"Msvm_TimeSyncComponentSettingData",
		"Msvm_VssComponentSettingData",
		"Msvm_GuestServiceInterfaceComponentSettingData",
	}
	seq := make([]string, 0, len(classes)*2)
	for _, cls := range classes {
		seq = append(seq, enumResponseGeneric, pulls[cls])
	}
	return seq
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

	// Hyper-V は WQL フィルタ列挙を拒否するため、Enumerate は無フィルタで送ること (#80)。
	if strings.Contains(bodies[0], "Filter") || strings.Contains(bodies[0], "SELECT") {
		t.Errorf("enumerate should be unfiltered (no WQL Filter); body: %s", bodies[0])
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
