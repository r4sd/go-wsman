package hyperv

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// bootSourceInstance は Msvm_BootSourceSettingData 1 件分の XML を組み立てる。
func bootSourceInstance(instanceID string, bootSourceType uint32, bootSourceDescription string) string {
	return fmt.Sprintf(`        <p:Msvm_BootSourceSettingData>
          <p:InstanceID>%s</p:InstanceID>
          <p:BootSourceType>%d</p:BootSourceType>
          <p:BootSourceDescription>%s</p:BootSourceDescription>
        </p:Msvm_BootSourceSettingData>`, instanceID, bootSourceType, bootSourceDescription)
}

func TestClient_ListBootSources(t *testing.T) {
	const vm = "11111111-aaaa-bbbb-cccc-000000000001"
	const other = "22222222-aaaa-bbbb-cccc-000000000002"
	pull := compPull("Msvm_BootSourceSettingData",
		bootSourceInstance("Microsoft:"+vm+`\nic-1\B`, BootSourceTypeNetwork, "EFI Network"),
		bootSourceInstance("Microsoft:"+vm+`\scsi-1\0\0\D\B`, BootSourceTypeDrive, "EFI SCSI Device"),
		bootSourceInstance("Microsoft:"+other+`\nic-1\B`, BootSourceTypeNetwork, "EFI Network"), // 別 VM、除外対象
	)

	var bodies []string
	server := newSequenceServer(t, []string{enumResponseGeneric, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListBootSources(context.Background(), vm)
	if err != nil {
		t.Fatalf("ListBootSources: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len: got %d, want 2 (別 VM は除外)", len(got))
	}
	if got[0].BootSourceType != BootSourceTypeNetwork || got[0].BootSourceDescription != "EFI Network" {
		t.Errorf("got[0]: %+v", got[0])
	}
	if got[1].BootSourceType != BootSourceTypeDrive || got[1].BootSourceDescription != "EFI SCSI Device" {
		t.Errorf("got[1]: %+v", got[1])
	}

	for i, b := range bodies {
		if strings.Contains(b, "Filter") || strings.Contains(b, "SELECT") {
			t.Errorf("enumerate should be unfiltered; bodies[%d]: %s", i, b)
		}
	}
}

func TestClient_ListBootSources_Empty(t *testing.T) {
	pull := compPull("Msvm_BootSourceSettingData")
	var bodies []string
	server := newSequenceServer(t, []string{enumResponseGeneric, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListBootSources(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("ListBootSources: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len: got %d, want 0", len(got))
	}
}

func TestClient_ListBootSources_EmptyVMGUID(t *testing.T) {
	client, _ := NewClient("https://example.invalid:5986/wsman")
	if _, err := client.ListBootSources(context.Background(), ""); err == nil {
		t.Fatal("空 vmGUID はエラーになるべき")
	}
}

// TestClient_BootSourceRef は BootSourceOrder[] へ書き込む WMI オブジェクトパス参照文字列の形式を
// 検証する (resolveBootOrders の実機確認済み対応規則の逆変換、deviceInstanceID + "\B")。
func TestClient_BootSourceRef(t *testing.T) {
	client, _ := NewClient("http://example.invalid/wsman")
	deviceInstanceID := `Microsoft:11111111-aaaa-bbbb-cccc-000000000001\nic-guid`
	got := client.BootSourceRef(deviceInstanceID)
	want := `Msvm_BootSourceSettingData.InstanceID="Microsoft:11111111-aaaa-bbbb-cccc-000000000001\\nic-guid\\B"`
	if !strings.HasSuffix(got, want) {
		t.Errorf("BootSourceRef:\ngot  %s\nwant suffix %s", got, want)
	}
}
