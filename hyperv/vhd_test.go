package hyperv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestClient_GetVirtualHardDisk は VHD ファイルパスから設定情報を取得するテスト。
func TestClient_GetVirtualHardDisk(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_get_vhd.xml")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Errorf("read body: %v", err)
		}
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(respXML))
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := client.GetVirtualHardDisk(context.Background(), `D:\VMs\test.vhdx`)
	if err != nil {
		t.Fatalf("GetVirtualHardDisk: %v", err)
	}

	if got.Path != `D:\VMs\test.vhdx` {
		t.Errorf("Path: got %q", got.Path)
	}
	if got.VirtualDiskFormat != VHDFormatVHDX {
		t.Errorf("VirtualDiskFormat: got %d", got.VirtualDiskFormat)
	}
	if got.VirtualDiskType != VHDTypeDynamic {
		t.Errorf("VirtualDiskType: got %d", got.VirtualDiskType)
	}
	if got.MaxInternalSize != 10737418240 {
		t.Errorf("MaxInternalSize: got %d", got.MaxInternalSize)
	}
}

// TestClient_CreateVirtualHardDisk は VHD 作成リクエストが正しく組み立てられ、非同期 Job
// (Msvm_StorageJob) の完了を内部で待つことを検証する。
//
// 想定リクエスト: 1) CreateVirtualHardDisk invoke (4096 + Job EPR)、2) Job の Get (完了)。
// WaitForJobEPR が Job EPR の ResourceURI (Msvm_StorageJob) を使って Get することも検証する。
func TestClient_CreateVirtualHardDisk(t *testing.T) {
	invokeResp := loadGolden(t, "invoke_response_create_vhd.xml")
	jobResp := loadGolden(t, "get_response_concretejob_completed.xml")

	var bodies []string
	server := newSequenceServer(t, []string{invokeResp, jobResp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	settings := Msvm_VirtualHardDiskSettingData{
		VirtualDiskFormat: VHDFormatVHDX,
		VirtualDiskType:   VHDTypeDynamic,
		Path:              `D:\VMs\new.vhdx`,
		MaxInternalSize:   10737418240,
	}

	jobRef, err := client.CreateVirtualHardDisk(context.Background(), &settings)
	if err != nil {
		t.Fatalf("CreateVirtualHardDisk: %v", err)
	}
	// 内部で Job 完了まで待つため、戻りの Job 参照は空。
	if jobRef != "" {
		t.Errorf("待機済みなので jobRef は空のはず, got %q", jobRef)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 requests (invoke + job get), got %d", len(bodies))
	}

	// 1 番目 (invoke) に VirtualDiskSettingData / Path / メソッド名が含まれること。
	if !strings.Contains(bodies[0], "VirtualDiskSettingData") || !strings.Contains(bodies[0], `D:\VMs\new.vhdx`) ||
		!strings.Contains(bodies[0], "CreateVirtualHardDisk") {
		t.Errorf("invoke body に必要な要素が無い")
	}
	// 2 番目 (Job Get) が Msvm_StorageJob URI を使っていること (ConcreteJob ではない)。
	if !strings.Contains(bodies[1], "Msvm_StorageJob") {
		t.Errorf("Job Get は Msvm_StorageJob URI を使うべき; body: %s", bodies[1])
	}
}

// TestClient_ResizeVirtualHardDisk は Resize リクエストの組み立てと Job (StorageJob) 待機を検証する。
func TestClient_ResizeVirtualHardDisk(t *testing.T) {
	invokeResp := loadGolden(t, "invoke_response_resize_vhd.xml")
	jobResp := loadGolden(t, "get_response_concretejob_completed.xml")

	var bodies []string
	server := newSequenceServer(t, []string{invokeResp, jobResp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	const (
		path    = `D:\VMs\resize.vhdx`
		newSize = uint64(21474836480) // 20 GiB
	)

	jobRef, err := client.ResizeVirtualHardDisk(context.Background(), path, newSize)
	if err != nil {
		t.Fatalf("ResizeVirtualHardDisk: %v", err)
	}
	if jobRef != "" {
		t.Errorf("待機済みなので jobRef は空のはず, got %q", jobRef)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(bodies))
	}
	if !strings.Contains(bodies[0], "ResizeVirtualHardDisk") || !strings.Contains(bodies[0], path) ||
		!strings.Contains(bodies[0], "21474836480") {
		t.Errorf("invoke body に必要な要素が無い")
	}
	if !strings.Contains(bodies[1], "Msvm_StorageJob") {
		t.Errorf("Job Get は Msvm_StorageJob URI を使うべき; body: %s", bodies[1])
	}
}

// TestClient_CreateVirtualHardDisk_JobGone は Job の Get が DestinationUnreachable (不在) を返したら
// エラーになることを検証する (旧 isJobGoneFault ハックの回帰。消えた Job を成功偽装しない)。
func TestClient_CreateVirtualHardDisk_JobGone(t *testing.T) {
	invokeResp := loadGolden(t, "invoke_response_create_vhd.xml")
	faultResp := loadGolden(t, "fault_destination_unreachable.xml")

	var bodies []string
	server := newSequenceServer(t, []string{invokeResp, faultResp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	settings := Msvm_VirtualHardDiskSettingData{
		VirtualDiskFormat: VHDFormatVHDX, VirtualDiskType: VHDTypeDynamic,
		Path: `D:\VMs\new.vhdx`, MaxInternalSize: 1 << 30,
	}
	if _, err := client.CreateVirtualHardDisk(context.Background(), &settings); err == nil {
		t.Fatal("Job が見つからない場合はエラーになるべき (成功偽装しない)")
	}
}

// TestClient_ResizeVirtualHardDisk_EmptyPath は Path 未指定時に
// パラメータ検証エラーになることを確認する。
func TestClient_ResizeVirtualHardDisk_EmptyPath(t *testing.T) {
	client, err := NewClient("http://example.invalid")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.ResizeVirtualHardDisk(context.Background(), "", 1024)
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
	if !strings.Contains(err.Error(), "path") {
		t.Errorf("error should mention path, got: %v", err)
	}
}
