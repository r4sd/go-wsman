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

	got, err := client.GetVirtualHardDisk(context.Background(), `C:\VMs\test.vhdx`)
	if err != nil {
		t.Fatalf("GetVirtualHardDisk: %v", err)
	}

	if got.Path != `C:\VMs\test.vhdx` {
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

// TestClient_CreateVirtualHardDisk は VHD 作成リクエストが正しく組み立てられるテスト。
func TestClient_CreateVirtualHardDisk(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_create_vhd.xml")

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

	settings := Msvm_VirtualHardDiskSettingData{
		VirtualDiskFormat: VHDFormatVHDX,
		VirtualDiskType:   VHDTypeDynamic,
		Path:              `C:\VMs\new.vhdx`,
		MaxInternalSize:   10737418240,
	}

	jobRef, err := client.CreateVirtualHardDisk(context.Background(), &settings)
	if err != nil {
		t.Fatalf("CreateVirtualHardDisk: %v", err)
	}

	// 非同期 Job が返されることを期待（ReturnValue=4096）
	if jobRef == "" {
		t.Errorf("expected job reference, got empty string")
	}

	// リクエストボディに VirtualDiskSettingData が含まれること
	if !strings.Contains(capturedBody, "VirtualDiskSettingData") {
		t.Errorf("request body should contain VirtualDiskSettingData parameter")
	}
	if !strings.Contains(capturedBody, `C:\VMs\new.vhdx`) {
		t.Errorf("request body should contain Path value")
	}
	if !strings.Contains(capturedBody, "CreateVirtualHardDisk") {
		t.Errorf("request body should contain method name")
	}
}
