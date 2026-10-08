package hyperv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// TestClient_GetSystemSettingData は VM GUID から Realized 構成を取得するテスト。
//
// Hyper-V は WQL フィルタ列挙を拒否する (#80) ため、無フィルタ列挙 + Go 側フィルタで
// 「対象 VM かつ VirtualSystemType=Realized」だけを選ぶ。mixed golden には同一 VM の
// Snapshot:Realized と別 VM の Realized も含めて、正しく Realized 1 件だけ選べることを検証する。
func TestClient_GetSystemSettingData(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_systemsettingdata.xml")
	pullXML := loadGolden(t, "pull_response_systemsettingdata_mixed.xml")

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

	// 対象 VM の Realized が選ばれること (Snapshot:Realized や別 VM ではない)。
	if got.VirtualSystemIdentifier != "11111111-aaaa-bbbb-cccc-000000000001" {
		t.Errorf("VirtualSystemIdentifier: got %q", got.VirtualSystemIdentifier)
	}
	if got.VirtualSystemType != VirtualSystemTypeRealized {
		t.Errorf("VirtualSystemType: got %q, want Realized (Snapshot を選んではいけない)", got.VirtualSystemType)
	}
	if got.ElementName != "vm-1" {
		t.Errorf("ElementName: got %q, want vm-1 (checkpoint を選んではいけない)", got.ElementName)
	}
	if got.VirtualSystemSubType != VirtualSystemSubTypeGen2 {
		t.Errorf("VirtualSystemSubType: got %q", got.VirtualSystemSubType)
	}
	if got.AutomaticStartupAction != AutomaticStartupActionRestartIfPreviouslyRunning {
		t.Errorf("AutomaticStartupAction: got %d", got.AutomaticStartupAction)
	}
	// ポインタ化済み (#149)。
	if got.SecureBoot == nil {
		t.Errorf("SecureBoot: nil (golden にプロパティがあるのに埋まっていない)")
	} else if !*got.SecureBoot {
		t.Errorf("SecureBoot: got false, want true")
	}

	// Hyper-V は WQL フィルタ列挙を拒否するため、Enumerate は無フィルタで送ること (#80)。
	if strings.Contains(enumBody, "Filter") || strings.Contains(enumBody, "SELECT") {
		t.Errorf("enumerate should be unfiltered (no WQL Filter); body: %s", enumBody)
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
		// ポインタフィールドの明示 false。create 経路でも握り潰されないこと (#135 / #149)。
		AutomaticSnapshotsEnabled: &falseVal,
		SecureBoot:                &falseVal,
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
	// 配線の検証: create 経路が marshal 直前にポインタを nil にする変異を検出する (#135 / #149)。
	for _, wantProp := range []string{
		`<PROPERTY NAME="AutomaticSnapshotsEnabled" TYPE="boolean"><VALUE>false</VALUE></PROPERTY>`,
		`<PROPERTY NAME="SecureBootEnabled" TYPE="boolean"><VALUE>false</VALUE></PROPERTY>`,
	} {
		if !strings.Contains(capturedBody, wantProp) {
			t.Errorf("明示的な false が DefineSystem の body に乗っていない\n want: %s\n body: %s", wantProp, capturedBody)
		}
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

// TestClient_ListSystemSettingData は全 VM の Realized 構成を取得するテスト。
//
// 無フィルタ列挙 + Go 側フィルタ (#80) で VirtualSystemType=Realized のみを返す。
// mixed golden は vm-1(Realized) / vm-1(Snapshot:Realized) / vm-2(Realized) を含むので、
// Snapshot を除外して Realized 2 件だけ返ることを検証する。
func TestClient_ListSystemSettingData(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_systemsettingdata.xml")
	pullXML := loadGolden(t, "pull_response_systemsettingdata_mixed.xml")

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
	// golden に SecureBootEnabled=FALSE があるので nil は「読めていない」= バグ。
	// `!= nil && *x` だけだと全件 nil にする変異が素通りする。
	if got[1].SecureBoot == nil {
		t.Errorf("got[1].SecureBoot: nil (golden にプロパティがあるのに埋まっていない)")
	} else if *got[1].SecureBoot {
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
	// Notes は実機では単一要素。複数行は改行を含む 1 要素で表現される。
	// 複数要素を送ると ReturnValue=0 のまま先頭だけが残る (2026-09-27 実機確認、#155)。
	// 値に & を含めていないのは、& 入りの Notes は実機が 32768 で拒否するため。
	if want := []string{"line1\nline2"}; !stringSlicesEqual(got.Notes, want) {
		t.Errorf("Notes: got %q, want %q", got.Notes, want)
	}

	// 数値フィールド
	if got.AutomaticCriticalErrorAction != AutomaticCriticalErrorActionPause {
		t.Errorf("AutomaticCriticalErrorAction: got %d, want %d",
			got.AutomaticCriticalErrorAction, AutomaticCriticalErrorActionPause)
	}
	// read は ISO 8601 duration (実機確認 2026-09-13)。write の CIM ネイティブ書式とは別 (#119)。
	if got.AutomaticCriticalErrorActionTimeout != "P0DT0H30M0S" {
		t.Errorf("AutomaticCriticalErrorActionTimeout: got %q", got.AutomaticCriticalErrorActionTimeout)
	}
	if got.AutomaticStartupActionDelay != "P0DT0H0M0S" {
		t.Errorf("AutomaticStartupActionDelay: got %q", got.AutomaticStartupActionDelay)
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
	// ポインタ化済み (#135)。nil = 応答にプロパティが無かった、と区別する。
	if got.AutomaticSnapshotsEnabled == nil {
		t.Errorf("AutomaticSnapshotsEnabled: nil (golden にプロパティがあるのに埋まっていない)")
	} else if !*got.AutomaticSnapshotsEnabled {
		t.Errorf("AutomaticSnapshotsEnabled: want true")
	}
	if !got.PauseAfterBootFailure {
		t.Errorf("PauseAfterBootFailure: want true")
	}
	if got.UserSnapshotType != UserSnapshotTypeProductionNoFallback {
		t.Errorf("UserSnapshotType: got %d, want %d (ProductionNoFallback)",
			got.UserSnapshotType, UserSnapshotTypeProductionNoFallback)
	}

	// Gen2 ファームウェア enum (#51)
	if got.NetworkBootPreferredProtocol != NetworkBootPreferredProtocolIPv6 {
		t.Errorf("NetworkBootPreferredProtocol: got %d, want %d (IPv6)",
			got.NetworkBootPreferredProtocol, NetworkBootPreferredProtocolIPv6)
	}
	if got.ConsoleMode != ConsoleModeCOM1 {
		t.Errorf("ConsoleMode: got %d, want %d (COM1)", got.ConsoleMode, ConsoleModeCOM1)
	}

	// string フィールド
	if got.SnapshotDataRoot != `D:\VMs\Snapshots\vm-full` {
		t.Errorf("SnapshotDataRoot: got %q", got.SnapshotDataRoot)
	}
}

// TestClient_UpdateVm は VM 設定の変更リクエストが正しく組み立てられ、
// 非同期 Job 参照が返ることを検証する (#50 part 2/2)。
//
// CIM の Msvm_VirtualSystemManagementService.ModifySystemSettings を叩く。
// SystemSettings には Msvm_VirtualSystemSettingData の embedded instance を入れ、
// InstanceID で対象 VM を特定する。
func TestClient_UpdateVm(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_modify_system_settings.xml")

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
		InstanceID: `Microsoft:11111111-2222-3333-4444-555555555555`,
		// 部分更新: ゼロ値のフィールドは embedded instance に出力されない
		// (marshalEmbeddedInstance の挙動)。下記2件だけが実際に CIM に送られる。
		Notes:                        []string{"updated by go-wsman test"},
		LockOnDisconnect:             true,
		AutomaticCriticalErrorAction: 1, // 1 = Pause (CIM 定数)
		// ポインタフィールドの明示 false。ここが送られないと #135 / #149 が直っていない。
		AutomaticSnapshotsEnabled: &falseVal,
		SecureBoot:                &falseVal,
	}

	jobRef, err := client.UpdateVm(context.Background(), settings)
	if err != nil {
		t.Fatalf("UpdateVm: %v", err)
	}

	if jobRef == "" {
		t.Errorf("expected job reference, got empty string")
	}

	// リクエスト body の中身を検証 (CIM メソッド名と embedded instance の主要要素)
	if !strings.Contains(capturedBody, "ModifySystemSettings") {
		t.Errorf("request body should contain method name ModifySystemSettings")
	}
	if !strings.Contains(capturedBody, "SystemSettings") {
		t.Errorf("request body should contain SystemSettings parameter")
	}
	if !strings.Contains(capturedBody, "Msvm_VirtualSystemSettingData") {
		t.Errorf("request body should contain embedded class name")
	}
	if !strings.Contains(capturedBody, settings.InstanceID) {
		t.Errorf("request body should contain InstanceID %q", settings.InstanceID)
	}
	if !strings.Contains(capturedBody, "updated by go-wsman test") {
		t.Errorf("request body should contain Notes value")
	}
	// 配線の検証: 純関数 (marshalEmbeddedInstance) のテストだけだと、
	// UpdateVm / clearReadOnlyForModify がポインタを握り潰す変異を検出できない (#135)。
	for _, wantProp := range []string{
		`<PROPERTY NAME="AutomaticSnapshotsEnabled" TYPE="boolean"><VALUE>false</VALUE></PROPERTY>`,
		`<PROPERTY NAME="SecureBootEnabled" TYPE="boolean"><VALUE>false</VALUE></PROPERTY>`,
	} {
		if !strings.Contains(capturedBody, wantProp) {
			t.Errorf("明示的な false が UpdateVm の body に乗っていない\n want: %s\n body: %s", wantProp, capturedBody)
		}
	}
}

// falseVal はポインタフィールドへ &false を渡すための変数。
var falseVal = false

// TestClient_UpdateVm_NilSettings は nil ポインタを渡した時にバリデーションエラーになることを確認する。
func TestClient_UpdateVm_NilSettings(t *testing.T) {
	client, err := NewClient("http://example.invalid")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.UpdateVm(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil settings, got nil")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("error should mention nil, got: %v", err)
	}
}

// TestClient_UpdateVm_EmptyInstanceID は InstanceID 未指定時にバリデーションエラーになることを確認する。
// CIM の ModifySystemSettings は InstanceID で更新対象を特定するため、空文字では呼び出してはならない。
func TestClient_UpdateVm_EmptyInstanceID(t *testing.T) {
	client, err := NewClient("http://example.invalid")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	settings := &Msvm_VirtualSystemSettingData{
		// InstanceID 空文字
		Notes: []string{"will not be sent"},
	}

	_, err = client.UpdateVm(context.Background(), settings)
	if err == nil {
		t.Fatal("expected error for empty InstanceID, got nil")
	}
	if !strings.Contains(err.Error(), "InstanceID") {
		t.Errorf("error should mention InstanceID, got: %v", err)
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

// clearReadOnlyForModify が「クリア対象だけをゼロにし、それ以外には一切触らない」ことを
// **reflect で全フィールド検証する**。
//
// 🔴 **手書きの一覧では #195 と同型の事故を守れない。** 以前はクリア対象を手で並べて
// `== ""` を見るだけだったので、Fable のレビューで次の変異がすべて生き残った:
//
//	SnapshotDataRoot と SwapFileDataRoot を入れ替える     → 両方非空なので通る
//	SnapshotDataRoot を別の値で上書きする                 → 非空なので通る
//	SecureBootTemplateId / BootSourceOrder も消す         → 一覧に無いので誰も見ない
//
// 3 つ目が #195 そのもの(**書けるフィールドが名前の類似で巻き込まれる**)。
// 全フィールドに非ゼロ値を入れてから呼び、クリア対象以外は**値がそのまま**であることを
// 見る形にすると、フィールドを足した時も分類を迫られる。
func TestClearReadOnlyForModify(t *testing.T) {
	// クリアされるべきフィールド。ここに無いものは**一切変わってはいけない**。
	//
	// SnapshotDataRoot / SwapFileDataRoot は **ModifySystemSettings で書ける**ので
	// 入っていない (#195。実機で 1 プロパティずつ確認済み)。
	wantCleared := map[string]bool{
		"VirtualSystemIdentifier": true,
		"VirtualSystemType":       true,
		"VirtualSystemSubType":    true,
		"ConfigurationID":         true,
		"ConfigurationDataRoot":   true,
		"ConfigurationFile":       true,
		"SuspendDataRoot":         true,
		"LogDataRoot":             true,
		"CreationTime":            true,
		"Parent":                  true,
		"Version":                 true,
		"Caption":                 true,
		"Description":             true,
	}

	before := &Msvm_VirtualSystemSettingData{}
	fillNonZero(t, reflect.ValueOf(before).Elem())
	after := *before // コピー (fillNonZero 後の値を保持)
	clearReadOnlyForModify(&after)

	rt := reflect.TypeOf(after)
	rvBefore := reflect.ValueOf(*before)
	rvAfter := reflect.ValueOf(after)
	seenCleared := 0
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		b := rvBefore.Field(i)
		a := rvAfter.Field(i)
		if wantCleared[name] {
			seenCleared++
			if !a.IsZero() {
				t.Errorf("%s が除去されていない: %#v", name, a.Interface())
			}
			continue
		}
		// クリア対象外は値がそのままであること。
		if !reflect.DeepEqual(b.Interface(), a.Interface()) {
			t.Errorf("%s を変えてはいけない: before=%#v after=%#v\n"+
				"  書き込み可能なフィールドを巻き込んでいないか確認すること (#195)",
				name, b.Interface(), a.Interface())
		}
	}
	// 一覧に書いたのに構造体に無い名前があると検査が空振りする。
	if seenCleared != len(wantCleared) {
		t.Errorf("wantCleared の %d 件のうち構造体で見つかったのは %d 件。"+
			"名前が変わった / フィールドが消えた可能性がある", len(wantCleared), seenCleared)
	}
}

// fillNonZero は構造体の全フィールドにゼロ値でない値を入れる。
//
// ゼロ値のままだと「クリアされた」と「もともと空だった」が区別できず、
// クリア対象の検査が空振りする。
func fillNonZero(t *testing.T, v reflect.Value) {
	t.Helper()
	rt := v.Type()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		name := rt.Field(i).Name
		switch f.Kind() {
		case reflect.String:
			f.SetString("nonzero-" + name)
		case reflect.Uint16, reflect.Uint32, reflect.Uint64:
			f.SetUint(uint64(i + 1)) // フィールドごとに違う値 (取り違えを検出するため)
		case reflect.Bool:
			f.SetBool(true)
		case reflect.Slice:
			if f.Type().Elem().Kind() != reflect.String {
				t.Fatalf("%s: 未対応の slice 要素型 %s。fillNonZero を拡張すること",
					name, f.Type().Elem())
			}
			f.Set(reflect.ValueOf([]string{"nonzero-" + name}))
		case reflect.Pointer:
			if f.Type().Elem().Kind() != reflect.Bool {
				t.Fatalf("%s: 未対応のポインタ型 %s。fillNonZero を拡張すること",
					name, f.Type().Elem())
			}
			b := true
			f.Set(reflect.ValueOf(&b))
		default:
			t.Fatalf("%s: 未対応の型 %s。fillNonZero を拡張すること", name, f.Type())
		}
	}
}
