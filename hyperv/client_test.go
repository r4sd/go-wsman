package hyperv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/r4sd/go-wsman/wsman"
)

func loadGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("failed to load golden file %s: %v", name, err)
	}
	return string(data)
}

// TestClient_GetComputerSystem は Get で単一 VM を取得するテスト。
func TestClient_GetComputerSystem(t *testing.T) {
	respXML := loadGolden(t, "get_response_computersystem.xml")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(respXML))
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := client.GetComputerSystem(context.Background(), "5C5E2D70-1111-2222-3333-444455556666")
	if err != nil {
		t.Fatalf("GetComputerSystem: %v", err)
	}

	if got.Name != "5C5E2D70-1111-2222-3333-444455556666" {
		t.Errorf("Name: got %q", got.Name)
	}
	if got.ElementName != "test-vm" {
		t.Errorf("ElementName: got %q", got.ElementName)
	}
	if got.EnabledState != EnabledStateEnabled {
		t.Errorf("EnabledState: got %d, want %d (Enabled)", got.EnabledState, EnabledStateEnabled)
	}
	if got.HealthState != 5 {
		t.Errorf("HealthState: got %d, want 5", got.HealthState)
	}
}

// TestClient_ListComputerSystems は Enumerate で全 VM を取得するテスト。
func TestClient_ListComputerSystems(t *testing.T) {
	enumXML := loadGolden(t, "enumerate_response_computersystem.xml")
	pullXML := loadGolden(t, "pull_response_computersystem.xml")

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

	got, err := client.ListComputerSystems(context.Background())
	if err != nil {
		t.Fatalf("ListComputerSystems: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len: got %d, want 2", len(got))
	}
	if got[0].ElementName != "vm-1" {
		t.Errorf("got[0].ElementName: got %q", got[0].ElementName)
	}
	if got[1].ElementName != "vm-2" {
		t.Errorf("got[1].ElementName: got %q", got[1].ElementName)
	}
	if got[0].EnabledState != EnabledStateEnabled {
		t.Errorf("got[0].EnabledState: got %d", got[0].EnabledState)
	}
	if got[1].EnabledState != EnabledStateDisabled {
		t.Errorf("got[1].EnabledState: got %d", got[1].EnabledState)
	}
}

// TestClient_NewClient は wsman.ClientOption が正しく伝播することを検証する。
func TestClient_NewClient_WithOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, wsman.WithTimeout(0))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client == nil {
		t.Fatal("client should not be nil")
	}
}
