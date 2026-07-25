package hyperv

import (
	"context"
	"strings"
	"testing"
)

// TestClient_SetIntegrationServiceEnabled は Heartbeat の EnabledState 変更 (Enable) を検証する。
// enumerate→pull で対象 VM の InstanceID を解決し、ModifyResourceSettings で最小 instance
// (InstanceID + EnabledState のみ) を送信、Job 完了まで待つ 4 リクエストの流れを確認する。
func TestClient_SetIntegrationServiceEnabled(t *testing.T) {
	const vm = "11111111-aaaa-bbbb-cccc-000000000001"
	id := "Microsoft:" + vm + "\\hb"
	pulls := map[string]string{
		"Msvm_HeartbeatComponentSettingData": compPull("Msvm_HeartbeatComponentSettingData",
			compInstance("Msvm_HeartbeatComponentSettingData", id, "Heartbeat", EnabledStateDisabled)),
	}
	invokeResp := loadGolden(t, "invoke_response_modify_resource_settings.xml")
	jobResp := loadGolden(t, "get_response_concretejob_completed.xml")

	var bodies []string
	seq := []string{enumResponseGeneric, pulls["Msvm_HeartbeatComponentSettingData"], invokeResp, jobResp}
	server := newSequenceServer(t, seq, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	err := client.SetIntegrationServiceEnabled(context.Background(), vm, IntegrationServiceHeartbeat, true)
	if err != nil {
		t.Fatalf("SetIntegrationServiceEnabled: %v", err)
	}
	if len(bodies) != 4 {
		t.Fatalf("expected 4 requests (enumerate+pull+invoke+job get), got %d", len(bodies))
	}

	// invoke (3番目) が対象 VM の InstanceID + EnabledState=2 (Enabled) のみを含む最小 instance であること。
	invokeBody := bodies[2]
	if !strings.Contains(invokeBody, "Msvm_HeartbeatComponentSettingData") {
		t.Errorf("invoke body should reference Msvm_HeartbeatComponentSettingData")
	}
	if !strings.Contains(invokeBody, id) {
		t.Errorf("invoke body should contain resolved InstanceID %q", id)
	}
	if !strings.Contains(invokeBody, `<PROPERTY NAME="EnabledState" TYPE="uint16"><VALUE>2</VALUE></PROPERTY>`) {
		t.Errorf("invoke body should set EnabledState=2 (Enabled); body: %s", invokeBody)
	}
	// ElementName はゼロ値 (未設定) なので送信されないこと (最小 instance の原則)。
	if strings.Contains(invokeBody, "ElementName") {
		t.Errorf("invoke body should NOT contain ElementName (minimal instance); body: %s", invokeBody)
	}
}

// TestClient_SetIntegrationServiceEnabled_Disable は Disable (EnabledState=3) が正しく送信されることを
// 検証する。EnabledState は Go のゼロ値 (0) を取らない値域 (2/3) のため、Slice A の processor で
// 発覚したゼロ値ダウングレード黙殺 (Fable C) の再発がないことを確認する回帰テスト。
func TestClient_SetIntegrationServiceEnabled_Disable(t *testing.T) {
	const vm = "11111111-aaaa-bbbb-cccc-000000000001"
	id := "Microsoft:" + vm + "\\hb"
	pulls := map[string]string{
		"Msvm_HeartbeatComponentSettingData": compPull("Msvm_HeartbeatComponentSettingData",
			compInstance("Msvm_HeartbeatComponentSettingData", id, "Heartbeat", EnabledStateEnabled)),
	}
	invokeResp := loadGolden(t, "invoke_response_modify_resource_settings.xml")
	jobResp := loadGolden(t, "get_response_concretejob_completed.xml")

	var bodies []string
	seq := []string{enumResponseGeneric, pulls["Msvm_HeartbeatComponentSettingData"], invokeResp, jobResp}
	server := newSequenceServer(t, seq, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	if err := client.SetIntegrationServiceEnabled(context.Background(), vm, IntegrationServiceHeartbeat, false); err != nil {
		t.Fatalf("SetIntegrationServiceEnabled(disable): %v", err)
	}

	invokeBody := bodies[2]
	if !strings.Contains(invokeBody, `<PROPERTY NAME="EnabledState" TYPE="uint16"><VALUE>3</VALUE></PROPERTY>`) {
		t.Errorf("invoke body should set EnabledState=3 (Disabled); body: %s", invokeBody)
	}
}

// TestClient_SetIntegrationServiceEnabled_NotFound は対象 VM に該当コンポーネントの割当が無い
// (別 VM のみ enumerate される) 場合にエラーを返すことを検証する。
func TestClient_SetIntegrationServiceEnabled_NotFound(t *testing.T) {
	const vm = "11111111-aaaa-bbbb-cccc-000000000001"
	const other = "22222222-aaaa-bbbb-cccc-000000000002"
	id := "Microsoft:" + other + "\\hb"
	pull := compPull("Msvm_HeartbeatComponentSettingData",
		compInstance("Msvm_HeartbeatComponentSettingData", id, "Heartbeat", EnabledStateEnabled))

	var bodies []string
	server := newSequenceServer(t, []string{enumResponseGeneric, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	err := client.SetIntegrationServiceEnabled(context.Background(), vm, IntegrationServiceHeartbeat, true)
	if err == nil {
		t.Fatal("対象 VM に割当が無い場合はエラーになるべき")
	}
}

// TestClient_SetIntegrationServiceEnabled_EmptyVMGUID は空 vmGUID を弾くことを検証する。
func TestClient_SetIntegrationServiceEnabled_EmptyVMGUID(t *testing.T) {
	client, _ := NewClient("https://example.invalid:5986/wsman")
	if err := client.SetIntegrationServiceEnabled(context.Background(), "", IntegrationServiceHeartbeat, true); err == nil {
		t.Fatal("空 vmGUID はエラーになるべき")
	}
}

// TestClient_SetIntegrationServiceEnabled_UnknownComponent は未知の component 値を弾くことを検証する。
func TestClient_SetIntegrationServiceEnabled_UnknownComponent(t *testing.T) {
	client, _ := NewClient("https://example.invalid:5986/wsman")
	if err := client.SetIntegrationServiceEnabled(context.Background(), "vm", IntegrationServiceComponent("bogus"), true); err == nil {
		t.Fatal("未知の component はエラーになるべき")
	}
}

// TestClient_GetIntegrationServiceEnabled は ListIntegrationServices と異なり、ElementName
// (ローカライズされる実行時表示名) を経由せず component 指定で直接 EnabledState を読めることを
// 検証する。ElementName をわざと空文字 (Unmarshal されない想定の値) にしても component 指定の
// 読み取りには影響しないことで、ロケール非依存性を裏付ける。
func TestClient_GetIntegrationServiceEnabled(t *testing.T) {
	const vm = "11111111-aaaa-bbbb-cccc-000000000001"
	id := "Microsoft:" + vm + "\\hb"
	// ElementName にローカライズ文字列 (例: 日本語ホストの「ハートビート」) を入れても、
	// component 指定の読み取りは影響を受けないことを確認する。
	pull := compPull("Msvm_HeartbeatComponentSettingData",
		compInstance("Msvm_HeartbeatComponentSettingData", id, "ハートビート", EnabledStateEnabled))

	var bodies []string
	server := newSequenceServer(t, []string{enumResponseGeneric, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	enabled, err := client.GetIntegrationServiceEnabled(context.Background(), vm, IntegrationServiceHeartbeat)
	if err != nil {
		t.Fatalf("GetIntegrationServiceEnabled: %v", err)
	}
	if !enabled {
		t.Errorf("Enabled: got false, want true (ローカライズされた ElementName に影響されず読めるべき)")
	}
}

// TestClient_GetIntegrationServiceEnabled_NotFound は対象 VM に割当が無い場合にエラーを返すことを検証する。
func TestClient_GetIntegrationServiceEnabled_NotFound(t *testing.T) {
	const vm = "11111111-aaaa-bbbb-cccc-000000000001"
	const other = "22222222-aaaa-bbbb-cccc-000000000002"
	pull := compPull("Msvm_HeartbeatComponentSettingData",
		compInstance("Msvm_HeartbeatComponentSettingData", "Microsoft:"+other+"\\hb", "Heartbeat", EnabledStateEnabled))

	var bodies []string
	server := newSequenceServer(t, []string{enumResponseGeneric, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	if _, err := client.GetIntegrationServiceEnabled(context.Background(), vm, IntegrationServiceHeartbeat); err == nil {
		t.Fatal("対象 VM に割当が無い場合はエラーになるべき")
	}
}
