package hyperv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestClient_GetSystemSettingData は VM GUID から Realized 構成を取得するテスト。
//
// 内部では WQL フィルタ付き Enumerate → Pull のサイクルを実行する。
func TestClient_GetSystemSettingData(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_systemsettingdata.xml")
	pullXML := loadGolden(t, "pull_response_systemsettingdata.xml")

	var enumBody string
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		callCount++
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		if callCount == 1 {
			enumBody = string(body)
			_, _ = w.Write([]byte(enumXML))
		} else {
			_, _ = w.Write([]byte(pullXML))
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := client.GetSystemSettingData(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("GetSystemSettingData: %v", err)
	}

	if got.VirtualSystemIdentifier != "11111111-aaaa-bbbb-cccc-000000000001" {
		t.Errorf("VirtualSystemIdentifier: got %q", got.VirtualSystemIdentifier)
	}
	if got.VirtualSystemType != VirtualSystemTypeRealized {
		t.Errorf("VirtualSystemType: got %q", got.VirtualSystemType)
	}
	if got.VirtualSystemSubType != VirtualSystemSubTypeGen2 {
		t.Errorf("VirtualSystemSubType: got %q", got.VirtualSystemSubType)
	}
	if got.AutomaticStartupAction != AutomaticStartupActionRestartIfPreviouslyRunning {
		t.Errorf("AutomaticStartupAction: got %d", got.AutomaticStartupAction)
	}
	if !got.SecureBoot {
		t.Errorf("SecureBoot: got false, want true")
	}

	// WQL クエリが Enumerate リクエストに含まれていることを検証
	if !strings.Contains(enumBody, "VirtualSystemIdentifier") {
		t.Errorf("enumerate body should contain WQL filter on VirtualSystemIdentifier")
	}
	if !strings.Contains(enumBody, VirtualSystemTypeRealized) {
		t.Errorf("enumerate body should filter by VirtualSystemType=Realized")
	}
}

// TestClient_GetSystemSettingData_NotFound は VM が見つからない場合のエラーを検証する。
func TestClient_GetSystemSettingData_NotFound(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_systemsettingdata.xml")
	emptyPullXML := `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:e="http://schemas.xmlsoap.org/ws/2004/09/enumeration">
  <s:Header>
    <a:Action>http://schemas.xmlsoap.org/ws/2004/09/enumeration/PullResponse</a:Action>
  </s:Header>
  <s:Body>
    <e:PullResponse>
      <e:Items/>
      <e:EndOfSequence/>
    </e:PullResponse>
  </s:Body>
</s:Envelope>`

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		if callCount == 1 {
			_, _ = w.Write([]byte(enumXML))
		} else {
			_, _ = w.Write([]byte(emptyPullXML))
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetSystemSettingData(context.Background(), "non-existent-guid")
	if err == nil {
		t.Fatal("expected error for non-existent VM, got nil")
	}
	if !strings.Contains(err.Error(), "no Realized setting") {
		t.Errorf("error should indicate no Realized setting, got: %v", err)
	}
}

// TestClient_GetSystemSettingData_EmptyName は vmName が空のときに即エラーを返す。
func TestClient_GetSystemSettingData_EmptyName(t *testing.T) {
	client, err := NewClient("http://localhost")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.GetSystemSettingData(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty vmName, got nil")
	}
}

// TestClient_DefineSystem は VM 作成リクエストが正しく組み立てられ、
// レスポンスから ResultingSystem (VM GUID) と Job 参照を取り出せることを検証する。
func TestClient_DefineSystem(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_define_system.xml")

	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		capturedBody = string(body)
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(respXML))
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	settings := &Msvm_VirtualSystemSettingData{
		ElementName:          "test-vm-new",
		VirtualSystemSubType: VirtualSystemSubTypeGen2,
	}

	got, err := client.DefineSystem(context.Background(), settings)
	if err != nil {
		t.Fatalf("DefineSystem: %v", err)
	}

	if got.ReturnValue != "4096" {
		t.Errorf("ReturnValue: got %q, want 4096", got.ReturnValue)
	}
	// extractProperties の挙動上、EPR 内の最後の非空 CharData が抽出される。
	// Job の場合は Selector "InstanceID" の値、ResultingSystem の場合は Selector "Name" の値。
	if got.JobRef != "5A2C9F44-1111-2222-3333-444455556666" {
		t.Errorf("JobRef: got %q", got.JobRef)
	}
	if got.ResultingSystem != "7B1F8DCC-AAAA-BBBB-CCCC-FFFFFFFFFFFF" {
		t.Errorf("ResultingSystem: got %q", got.ResultingSystem)
	}

	// リクエストボディに SystemSettings + 表示名が含まれること
	if !strings.Contains(capturedBody, "SystemSettings") {
		t.Errorf("request body should contain SystemSettings parameter")
	}
	if !strings.Contains(capturedBody, "test-vm-new") {
		t.Errorf("request body should contain ElementName value")
	}
	if !strings.Contains(capturedBody, "DefineSystem") {
		t.Errorf("request body should contain method name")
	}
	if !strings.Contains(capturedBody, "Msvm_VirtualSystemSettingData") {
		t.Errorf("request body should embed Msvm_VirtualSystemSettingData class element")
	}
}

// TestClient_DefineSystem_Validation はバリデーションエラーを検証する。
func TestClient_DefineSystem_Validation(t *testing.T) {
	client, err := NewClient("http://localhost")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if _, err := client.DefineSystem(context.Background(), nil); err == nil {
		t.Error("expected error for nil settings")
	}

	if _, err := client.DefineSystem(context.Background(), &Msvm_VirtualSystemSettingData{}); err == nil {
		t.Error("expected error for empty ElementName")
	}
}

// TestClient_DestroySystem は VM 削除リクエストが正しく組み立てられることを検証する。
func TestClient_DestroySystem(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_destroy_system.xml")

	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		capturedBody = string(body)
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(respXML))
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	jobRef, err := client.DestroySystem(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("DestroySystem: %v", err)
	}

	if jobRef != "8E4D1A99-7777-8888-9999-AAAABBBBCCCC" {
		t.Errorf("jobRef: got %q", jobRef)
	}

	// リクエストボディに AffectedSystem (EPR) と対象 VM の GUID + Msvm_ComputerSystem URI が含まれること
	if !strings.Contains(capturedBody, "AffectedSystem") {
		t.Errorf("request body should contain AffectedSystem parameter")
	}
	if !strings.Contains(capturedBody, "11111111-aaaa-bbbb-cccc-000000000001") {
		t.Errorf("request body should contain target VM GUID")
	}
	if !strings.Contains(capturedBody, "Msvm_ComputerSystem") {
		t.Errorf("request body EPR should reference Msvm_ComputerSystem")
	}
	if !strings.Contains(capturedBody, "DestroySystem") {
		t.Errorf("request body should contain method name")
	}
}

// TestClient_DestroySystem_EmptyName は vmName 空のときに即エラーを返す。
func TestClient_DestroySystem_EmptyName(t *testing.T) {
	client, err := NewClient("http://localhost")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.DestroySystem(context.Background(), ""); err == nil {
		t.Error("expected error for empty vmName")
	}
}

// TestBuildEndpointReference は EPR 文字列の構造を検証する。
func TestBuildEndpointReference(t *testing.T) {
	epr := buildEndpointReference(msvmComputerSystemURI, map[string]string{
		"Name": "abc-123",
	})

	checks := []string{
		`<a:Address>http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</a:Address>`,
		`<w:Selector Name="Name">abc-123</w:Selector>`,
		"Msvm_ComputerSystem",
		"<a:ReferenceParameters>",
	}
	for _, want := range checks {
		if !strings.Contains(epr, want) {
			t.Errorf("EPR missing %q\nfull: %s", want, epr)
		}
	}
}

// TestClient_ListSystemSettingData は WQL で全 VM の Realized 構成を取得するテスト。
func TestClient_ListSystemSettingData(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_systemsettingdata.xml")
	pullXML := loadGolden(t, "pull_response_systemsettingdata.xml")

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		if callCount == 1 {
			_, _ = w.Write([]byte(enumXML))
		} else {
			_, _ = w.Write([]byte(pullXML))
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := client.ListSystemSettingData(context.Background())
	if err != nil {
		t.Fatalf("ListSystemSettingData: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len: got %d, want 2", len(got))
	}
	if got[0].ElementName != "vm-1" {
		t.Errorf("got[0].ElementName: got %q", got[0].ElementName)
	}
	if got[0].VirtualSystemSubType != VirtualSystemSubTypeGen2 {
		t.Errorf("got[0].VirtualSystemSubType: got %q", got[0].VirtualSystemSubType)
	}
	if got[1].ElementName != "vm-2" {
		t.Errorf("got[1].ElementName: got %q", got[1].ElementName)
	}
	if got[1].VirtualSystemSubType != VirtualSystemSubTypeGen1 {
		t.Errorf("got[1].VirtualSystemSubType: got %q", got[1].VirtualSystemSubType)
	}
	if got[1].SecureBoot {
		t.Errorf("got[1].SecureBoot: want false (Gen1 では SecureBoot 無効)")
	}
}

// TestClient_GetSystemSettingData_FullFields は #50 で追加した詳細フィールド
// (BootSourceOrder, Notes, AutomaticCriticalErrorAction 等) が正しく Unmarshal
// されることを検証する。
//
// 既存テスト用の簡易 golden file ではなく、新フィールド全てを含む
// pull_response_systemsettingdata_full.xml を使用する。
func TestClient_GetSystemSettingData_FullFields(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_systemsettingdata.xml")
	pullXML := loadGolden(t, "pull_response_systemsettingdata_full.xml")

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		if callCount == 1 {
			_, _ = w.Write([]byte(enumXML))
		} else {
			_, _ = w.Write([]byte(pullXML))
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := client.GetSystemSettingData(context.Background(), "99999999-aaaa-bbbb-cccc-000000000099")
	if err != nil {
		t.Fatalf("GetSystemSettingData: %v", err)
	}

	// 配列フィールド
	if want := []string{
		`Microsoft:99999999-aaaa-bbbb-cccc-000000000099\BootSource\0`,
		`Microsoft:99999999-aaaa-bbbb-cccc-000000000099\BootSource\1`,
		`Microsoft:99999999-aaaa-bbbb-cccc-000000000099\BootSource\2`,
	}; !stringSlicesEqual(got.BootSourceOrder, want) {
		t.Errorf("BootSourceOrder: got %v, want %v", got.BootSourceOrder, want)
	}
	if want := []string{"line1", "line2 with & symbol"}; !stringSlicesEqual(got.Notes, want) {
		t.Errorf("Notes: got %v, want %v", got.Notes, want)
	}

	// 数値フィールド
	if got.AutomaticCriticalErrorAction != AutomaticCriticalErrorActionPause {
		t.Errorf("AutomaticCriticalErrorAction: got %d, want %d",
			got.AutomaticCriticalErrorAction, AutomaticCriticalErrorActionPause)
	}
	if got.AutomaticCriticalErrorActionTimeout != "00000000003000.000000:000" {
		t.Errorf("AutomaticCriticalErrorActionTimeout: got %q", got.AutomaticCriticalErrorActionTimeout)
	}
	if got.HighMmioGapSize != 536870912 {
		t.Errorf("HighMmioGapSize: got %d", got.HighMmioGapSize)
	}
	if got.LowMmioGapSize != 268435456 {
		t.Errorf("LowMmioGapSize: got %d", got.LowMmioGapSize)
	}

	// bool フィールド
	if !got.LockOnDisconnect {
		t.Errorf("LockOnDisconnect: want true")
	}
	if got.GuestControlledCacheTypes {
		t.Errorf("GuestControlledCacheTypes: want false")
	}
	if !got.AutomaticSnapshotsEnabled {
		t.Errorf("AutomaticSnapshotsEnabled: want true")
	}

	// string フィールド
	if got.SnapshotDataRoot != `C:\VMs\Snapshots\vm-full` {
		t.Errorf("SnapshotDataRoot: got %q", got.SnapshotDataRoot)
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
