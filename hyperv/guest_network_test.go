package hyperv

import (
	"context"
	"strings"
	"testing"
)

// guestNetInstance は Msvm_GuestNetworkAdapterConfiguration 1 件分の XML を組み立てる
// (配列プロパティは要素ごとに repeated element)。
func guestNetInstance(instanceID string, dhcpEnabled bool, ips ...string) string {
	var sb strings.Builder
	sb.WriteString("        <p:Msvm_GuestNetworkAdapterConfiguration>\n")
	sb.WriteString("          <p:InstanceID>" + instanceID + "</p:InstanceID>\n")
	sb.WriteString("          <p:ProtocolIFType>4096</p:ProtocolIFType>\n")
	if dhcpEnabled {
		sb.WriteString("          <p:DHCPEnabled>TRUE</p:DHCPEnabled>\n")
	} else {
		sb.WriteString("          <p:DHCPEnabled>FALSE</p:DHCPEnabled>\n")
	}
	for _, ip := range ips {
		sb.WriteString("          <p:IPAddresses>" + ip + "</p:IPAddresses>\n")
	}
	sb.WriteString("        </p:Msvm_GuestNetworkAdapterConfiguration>")
	return sb.String()
}

// sdcInstance は Msvm_SettingDataComponent (association) 1 件分の XML を組み立てる。
// GroupComponent/PartComponent は素の InstanceID がそのまま返る (実機確認済み、hyperv/types.go の
// Msvm_SettingDataComponent コメント参照。Parent/HostResource の WMI オブジェクトパス文字列とは違う)。
func sdcInstance(groupComponentRef, partComponentRef string) string {
	return `        <p:Msvm_SettingDataComponent>
          <p:GroupComponent>` + groupComponentRef + `</p:GroupComponent>
          <p:PartComponent>` + partComponentRef + `</p:PartComponent>
        </p:Msvm_SettingDataComponent>`
}

func TestClient_ListGuestNetworkAdapterConfigurations(t *testing.T) {
	pull := compPull("Msvm_GuestNetworkAdapterConfiguration",
		guestNetInstance("guest-nic-1", true, "192.168.1.50", "fe80::1"),
		guestNetInstance("guest-nic-2", false),
	)
	var bodies []string
	server := newSequenceServer(t, []string{enumResponseGeneric, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListGuestNetworkAdapterConfigurations(context.Background())
	if err != nil {
		t.Fatalf("ListGuestNetworkAdapterConfigurations: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len: got %d, want 2", len(got))
	}
	if got[0].InstanceID != "guest-nic-1" || !got[0].DHCPEnabled {
		t.Errorf("got[0]: %+v", got[0])
	}
	if len(got[0].IPAddresses) != 2 || got[0].IPAddresses[0] != "192.168.1.50" || got[0].IPAddresses[1] != "fe80::1" {
		t.Errorf("got[0].IPAddresses: %v", got[0].IPAddresses)
	}
	if got[1].InstanceID != "guest-nic-2" || got[1].DHCPEnabled {
		t.Errorf("got[1]: %+v", got[1])
	}
	if len(got[1].IPAddresses) != 0 {
		t.Errorf("got[1].IPAddresses should be empty (DHCP待ちでIP未取得): %v", got[1].IPAddresses)
	}

	// Hyper-V は WQL フィルタ列挙を拒否するため無フィルタで送ること (#80 の教訓と同じ)。
	for i, b := range bodies {
		if strings.Contains(b, "Filter") || strings.Contains(b, "SELECT") {
			t.Errorf("enumerate should be unfiltered; bodies[%d]: %s", i, b)
		}
	}
}

func TestClient_ListGuestNetworkAdapterConfigurations_Empty(t *testing.T) {
	pull := compPull("Msvm_GuestNetworkAdapterConfiguration")
	var bodies []string
	server := newSequenceServer(t, []string{enumResponseGeneric, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListGuestNetworkAdapterConfigurations(context.Background())
	if err != nil {
		t.Fatalf("ListGuestNetworkAdapterConfigurations: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len: got %d, want 0", len(got))
	}
}

func TestClient_ListSettingDataComponents(t *testing.T) {
	// 実機確認済みの形式: 素の InstanceID (WMI オブジェクトパス文字列ではない)。
	const groupRef = `Microsoft:11111111-aaaa-bbbb-cccc-000000000001\port-1`
	const partRef = `Microsoft:GuestNetwork\11111111-aaaa-bbbb-cccc-000000000001\port-1`
	pull := compPull("Msvm_SettingDataComponent", sdcInstance(groupRef, partRef))

	var bodies []string
	server := newSequenceServer(t, []string{enumResponseGeneric, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListSettingDataComponents(context.Background())
	if err != nil {
		t.Fatalf("ListSettingDataComponents: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	if got[0].GroupComponent != groupRef {
		t.Errorf("GroupComponent: got %q, want %q", got[0].GroupComponent, groupRef)
	}
	if got[0].PartComponent != partRef {
		t.Errorf("PartComponent: got %q, want %q", got[0].PartComponent, partRef)
	}
}

func TestClient_ListSettingDataComponents_Empty(t *testing.T) {
	pull := compPull("Msvm_SettingDataComponent")
	var bodies []string
	server := newSequenceServer(t, []string{enumResponseGeneric, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListSettingDataComponents(context.Background())
	if err != nil {
		t.Fatalf("ListSettingDataComponents: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len: got %d, want 0", len(got))
	}
}
