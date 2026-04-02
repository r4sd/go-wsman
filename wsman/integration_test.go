//go:build integration

package wsman

import (
	"context"
	"os"
	"testing"
	"time"
)

// Integration Test は実際の WinRM ホストに接続してテストする。
//
// 実行方法:
//
//	WSMAN_ENDPOINT=https://10.0.0.100:5986/wsman \
//	WSMAN_USERNAME=terraform \
//	WSMAN_PASSWORD=yourpassword \
//	go test -race -tags=integration -v ./wsman/...
//
// 環境変数:
//   - WSMAN_ENDPOINT: WinRM エンドポイント URL（必須）
//   - WSMAN_USERNAME: NTLM 認証ユーザー名（必須）
//   - WSMAN_PASSWORD: NTLM 認証パスワード（必須）

func getIntegrationClient(t *testing.T) *Client {
	t.Helper()

	endpoint := os.Getenv("WSMAN_ENDPOINT")
	username := os.Getenv("WSMAN_USERNAME")
	password := os.Getenv("WSMAN_PASSWORD")

	if endpoint == "" || username == "" || password == "" {
		t.Skip("WSMAN_ENDPOINT, WSMAN_USERNAME, WSMAN_PASSWORD must be set")
	}

	client, err := NewClient(endpoint, WithNTLM(username, password))
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	return client
}

func TestIntegration_Get_ComputerSystem(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Win32_ComputerSystem は singleton ではないため、SelectorSet（キー）が必要。
	// まず Enumerate でコンピュータ名を取得し、Get + Selector で取得する。
	resourceURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem"

	instances, err := client.Enumerate(ctx, resourceURI)
	if err != nil {
		t.Fatalf("Enumerate Win32_ComputerSystem failed: %v", err)
	}
	if len(instances) == 0 {
		t.Fatal("no ComputerSystem instances returned")
	}

	computerName := instances[0].Property("Name")
	if computerName == "" {
		t.Fatal("Name property from Enumerate is empty")
	}
	t.Logf("Discovered computer name: %q", computerName)

	// SelectorSet 付き Get で単一インスタンスを取得
	resp, err := client.Get(ctx, resourceURI,
		Selector{Name: "Name", Value: computerName},
	)
	if err != nil {
		t.Fatalf("Get Win32_ComputerSystem with selector failed: %v", err)
	}

	name := resp.Property("Name")
	if name != computerName {
		t.Errorf("Name = %q, want %q", name, computerName)
	}

	domain := resp.Property("Domain")
	t.Logf("ComputerSystem.Domain = %q", domain)

	totalMem := resp.Property("TotalPhysicalMemory")
	t.Logf("ComputerSystem.TotalPhysicalMemory = %s", totalMem)
}

func TestIntegration_Get_OperatingSystem(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Get(ctx,
		"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_OperatingSystem",
	)
	if err != nil {
		t.Fatalf("Get Win32_OperatingSystem failed: %v", err)
	}

	caption := resp.Property("Caption")
	if caption == "" {
		t.Error("Caption property is empty")
	}
	t.Logf("OperatingSystem.Caption = %q", caption)

	version := resp.Property("Version")
	t.Logf("OperatingSystem.Version = %q", version)
}

func TestIntegration_Enumerate_Processes(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	instances, err := client.Enumerate(ctx,
		"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Process",
	)
	if err != nil {
		t.Fatalf("Enumerate Win32_Process failed: %v", err)
	}

	if len(instances) == 0 {
		t.Fatal("no processes returned")
	}
	t.Logf("Process count: %d", len(instances))

	// 最初の 5 件のプロセス名を表示
	for i, inst := range instances {
		if i >= 5 {
			break
		}
		t.Logf("  [%d] Name=%q PID=%s", i, inst.Property("Name"), inst.Property("ProcessId"))
	}
}

func TestIntegration_Enumerate_Services(t *testing.T) {
	client := getIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	instances, err := client.Enumerate(ctx,
		"http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service",
	)
	if err != nil {
		t.Fatalf("Enumerate Win32_Service failed: %v", err)
	}

	if len(instances) == 0 {
		t.Fatal("no services returned")
	}
	t.Logf("Service count: %d", len(instances))

	// WinRM サービスを探す
	for _, inst := range instances {
		if inst.Property("Name") == "WinRM" {
			t.Logf("WinRM service: State=%q StartMode=%q",
				inst.Property("State"), inst.Property("StartMode"))
			return
		}
	}
	t.Log("WinRM service not found in list (might be named differently)")
}
