package hyperv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestClient_CreateVmCheckpoint はチェックポイント作成リクエストが正しく組み立てられ、
// レスポンスから Job 参照と作成されたチェックポイントの参照を取り出せることを検証する。
func TestClient_CreateVmCheckpoint(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_create_snapshot.xml")

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

	got, err := client.CreateVmCheckpoint(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001", SnapshotTypeFull)
	if err != nil {
		t.Fatalf("CreateVmCheckpoint: %v", err)
	}

	if got.ReturnValue != "4096" {
		t.Errorf("ReturnValue: got %q, want 4096", got.ReturnValue)
	}
	if got.JobRef != "1A2B3C4D-5555-6666-7777-888899990000" {
		t.Errorf("JobRef: got %q", got.JobRef)
	}
	// 実機は非同期 (4096) のとき ResultingSnapshot を返さない (2026-08-01 実機確認)。
	// 「SnapshotRef が取れること」を緑の条件にすると、実機に存在しない挙動を仕様として
	// 固定してしまう。作成したチェックポイントの特定方法は #125 で追跡。
	if got.SnapshotRef != "" {
		t.Errorf("SnapshotRef: got %q, want empty (非同期応答に ResultingSnapshot は含まれない)", got.SnapshotRef)
	}

	if !strings.Contains(capturedBody, "AffectedSystem") {
		t.Errorf("request body should contain AffectedSystem parameter")
	}
	if !strings.Contains(capturedBody, "11111111-aaaa-bbbb-cccc-000000000001") {
		t.Errorf("request body should contain target VM GUID")
	}
	if !strings.Contains(capturedBody, "SnapshotType") {
		t.Errorf("request body should contain SnapshotType parameter")
	}
	if !strings.Contains(capturedBody, ">2<") {
		t.Errorf("request body should contain SnapshotType value 2 (Full)")
	}
	if !strings.Contains(capturedBody, "CreateSnapshot") {
		t.Errorf("request body should contain method name")
	}
	if !strings.Contains(capturedBody, "Msvm_VirtualSystemSnapshotService") {
		t.Errorf("request body should target Msvm_VirtualSystemSnapshotService")
	}
}

// TestClient_CreateVmCheckpoint_EmptyName は vmName が空のときに即エラーを返す。
func TestClient_CreateVmCheckpoint_EmptyName(t *testing.T) {
	client, err := NewClient("http://localhost")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.CreateVmCheckpoint(context.Background(), "", SnapshotTypeFull); err == nil {
		t.Error("expected error for empty vmName")
	}
}

// TestClient_ApplyVmCheckpoint はチェックポイント復元リクエストが正しく組み立てられることを検証する。
func TestClient_ApplyVmCheckpoint(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_apply_snapshot.xml")

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

	checkpointID := `Microsoft:11111111-aaaa-bbbb-cccc-000000000001\SNAP-0001`
	jobRef, err := client.ApplyVmCheckpoint(context.Background(), checkpointID)
	if err != nil {
		t.Fatalf("ApplyVmCheckpoint: %v", err)
	}

	if jobRef != "2B3C4D5E-6666-7777-8888-999900001111" {
		t.Errorf("jobRef: got %q", jobRef)
	}

	if !strings.Contains(capturedBody, "Snapshot") {
		t.Errorf("request body should contain Snapshot parameter")
	}
	if !strings.Contains(capturedBody, "SNAP-0001") {
		t.Errorf("request body should contain target checkpoint InstanceID")
	}
	if !strings.Contains(capturedBody, "ApplySnapshot") {
		t.Errorf("request body should contain method name")
	}
	if !strings.Contains(capturedBody, "Msvm_VirtualSystemSettingData") {
		t.Errorf("request body EPR should reference Msvm_VirtualSystemSettingData")
	}
}

// TestClient_ApplyVmCheckpoint_EmptyID は checkpointInstanceID が空のときに即エラーを返す。
func TestClient_ApplyVmCheckpoint_EmptyID(t *testing.T) {
	client, err := NewClient("http://localhost")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.ApplyVmCheckpoint(context.Background(), ""); err == nil {
		t.Error("expected error for empty checkpointInstanceID")
	}
}

// TestClient_DestroyVmCheckpoint はチェックポイント削除リクエストが正しく組み立てられることを検証する。
func TestClient_DestroyVmCheckpoint(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_destroy_snapshot.xml")

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

	checkpointID := `Microsoft:11111111-aaaa-bbbb-cccc-000000000001\SNAP-0001`
	jobRef, err := client.DestroyVmCheckpoint(context.Background(), checkpointID)
	if err != nil {
		t.Fatalf("DestroyVmCheckpoint: %v", err)
	}

	if jobRef != "3C4D5E6F-7777-8888-9999-000011112222" {
		t.Errorf("jobRef: got %q", jobRef)
	}

	if !strings.Contains(capturedBody, "AffectedSnapshot") {
		t.Errorf("request body should contain AffectedSnapshot parameter")
	}
	if !strings.Contains(capturedBody, "SNAP-0001") {
		t.Errorf("request body should contain target checkpoint InstanceID")
	}
	if !strings.Contains(capturedBody, "DestroySnapshot") {
		t.Errorf("request body should contain method name")
	}
}

// TestClient_DestroyVmCheckpoint_EmptyID は checkpointInstanceID が空のときに即エラーを返す。
func TestClient_DestroyVmCheckpoint_EmptyID(t *testing.T) {
	client, err := NewClient("http://localhost")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.DestroyVmCheckpoint(context.Background(), ""); err == nil {
		t.Error("expected error for empty checkpointInstanceID")
	}
}

// TestClient_ListVmCheckpoints は VM のチェックポイント一覧が Snapshot:Realized だけ
// 正しく絞り込まれることを検証する (Realized 本体・別 VM を含めない)。
func TestClient_ListVmCheckpoints(t *testing.T) {
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

	got, err := client.ListVmCheckpoints(context.Background(), "11111111-aaaa-bbbb-cccc-000000000001")
	if err != nil {
		t.Fatalf("ListVmCheckpoints: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("len(got): got %d, want 1 (Realized 本体・別 VM を含めてはいけない)", len(got))
	}
	if got[0].ElementName != "vm-1 checkpoint" {
		t.Errorf("ElementName: got %q", got[0].ElementName)
	}
	if got[0].VirtualSystemType != VirtualSystemTypeSnapshotRealized {
		t.Errorf("VirtualSystemType: got %q", got[0].VirtualSystemType)
	}
}

// TestClient_ListVmCheckpoints_EmptyName は vmName が空のときに即エラーを返す。
func TestClient_ListVmCheckpoints_EmptyName(t *testing.T) {
	client, err := NewClient("http://localhost")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.ListVmCheckpoints(context.Background(), ""); err == nil {
		t.Error("expected error for empty vmName")
	}
}

// TestClient_RenameVmCheckpoint はチェックポイントのリネーム要求が
// ModifySystemSettings として組み立てられ、InstanceID + ElementName だけの
// 最小インスタンスが送られることを検証する。
func TestClient_RenameVmCheckpoint(t *testing.T) {
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

	const checkpointID = "Microsoft:C7AA2991-F64D-4B6C-A8AD-D5EB5107B68A"
	jobRef, err := client.RenameVmCheckpoint(context.Background(), checkpointID, "my-checkpoint")
	if err != nil {
		t.Fatalf("RenameVmCheckpoint: %v", err)
	}
	if jobRef == "" {
		t.Error("expected job reference, got empty string")
	}

	if !strings.Contains(capturedBody, "ModifySystemSettings") {
		t.Errorf("request body should contain method name ModifySystemSettings")
	}
	if !strings.Contains(capturedBody, checkpointID) {
		t.Errorf("request body should contain checkpoint InstanceID")
	}
	if !strings.Contains(capturedBody, "my-checkpoint") {
		t.Errorf("request body should contain the new ElementName")
	}
}

// TestClient_RenameVmCheckpoint_Validation は引数が空のときに即エラーを返す。
func TestClient_RenameVmCheckpoint_Validation(t *testing.T) {
	client, err := NewClient("http://localhost")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.RenameVmCheckpoint(context.Background(), "", "name"); err == nil {
		t.Error("expected error for empty checkpointInstanceID")
	}
	if _, err := client.RenameVmCheckpoint(context.Background(), "Microsoft:xxx", ""); err == nil {
		t.Error("expected error for empty newName")
	}
}
