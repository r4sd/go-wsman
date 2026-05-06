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
