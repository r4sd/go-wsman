package hyperv

import (
	"context"
	"fmt"
	"strconv"

	"github.com/r4sd/go-wsman/wsman"
)

// RequestedState 定数（CIM 仕様: Msvm_ComputerSystem.RequestStateChange の RequestedState）
//
// Hyper-V は CIM 標準の状態遷移値に加えて拡張値 (32768/32769) も受け付ける。
// 値の意味は Msvm_ComputerSystem.EnabledState と一部対応するが、
// 「遷移先」を指す入力値であり EnabledState の現在値定数とは分けて管理する。
const (
	RequestedStateEnabled  uint16 = 2     // Start: VM を起動
	RequestedStateDisabled uint16 = 3     // TurnOff: 強制電源断 (Hyper-V のシャットダウンではない)
	RequestedStateShutDown uint16 = 4     // Shutdown: ゲスト OS のシャットダウンを要求 (Integration Services 必須)
	RequestedStateReboot   uint16 = 10    // Reboot: 強制再起動
	RequestedStateReset    uint16 = 11    // Reset
	RequestedStatePaused   uint16 = 32768 // Pause: 一時停止
	RequestedStateSaved    uint16 = 32769 // Save: 状態を保存して停止
)

// RequestStateChange は Msvm_ComputerSystem.RequestStateChange を呼び出し、
// VM の状態遷移を要求する。
//
// vmName は Msvm_ComputerSystem.Name (VM GUID)。
// state は RequestedState* 定数のいずれか。
//
// 戻り値は非同期 Job 参照。ReturnValue=4096 の場合は Job 完了まで遷移は未完了。
// 同期成功 (ReturnValue=0) の場合は jobRef は空文字列。
//
// 注意: RequestStateChange はインスタンスメソッドなので Selector{Name="Name"} で
// 対象 VM を特定する。
func (c *Client) RequestStateChange(ctx context.Context, vmName string, state uint16) (string, error) {
	if vmName == "" {
		return "", fmt.Errorf("RequestStateChange: vmName must not be empty")
	}

	resp, err := c.wsman.Invoke(ctx, msvmComputerSystemURI, "RequestStateChange",
		map[string]string{"RequestedState": strconv.FormatUint(uint64(state), 10)},
		wsman.Selector{Name: "Name", Value: vmName},
	)
	if err != nil {
		return "", err
	}

	rv := resp.ReturnValue
	// CIM 仕様: 0=Completed, 4096=Method parameters checked - job started.
	// その他は失敗 (1=Not Supported, 2=Unknown/Unspecified, 4=Failed 等)。
	if rv != "0" && rv != "4096" {
		return "", fmt.Errorf("RequestStateChange: unexpected ReturnValue=%s", rv)
	}

	jobRef := resp.Property("Job")
	if rv == "4096" && jobRef == "" {
		return "", fmt.Errorf("RequestStateChange: ReturnValue=4096 but no Job reference")
	}
	return jobRef, nil
}

// StartVM は VM を起動する。RequestStateChange(Enabled=2) のショートカット。
func (c *Client) StartVM(ctx context.Context, vmName string) (string, error) {
	return c.RequestStateChange(ctx, vmName, RequestedStateEnabled)
}

// TurnOffVM は VM を強制電源断する。RequestStateChange(Disabled=3) のショートカット。
//
// ゲスト OS の正常シャットダウンを行いたい場合は ShutdownVM を使う。
func (c *Client) TurnOffVM(ctx context.Context, vmName string) (string, error) {
	return c.RequestStateChange(ctx, vmName, RequestedStateDisabled)
}

// ShutdownVM はゲスト OS にシャットダウンを要求する。
// Integration Services が動作していない VM では失敗する。
func (c *Client) ShutdownVM(ctx context.Context, vmName string) (string, error) {
	return c.RequestStateChange(ctx, vmName, RequestedStateShutDown)
}

// PauseVM は VM を一時停止する。RequestStateChange(Paused=32768) のショートカット。
func (c *Client) PauseVM(ctx context.Context, vmName string) (string, error) {
	return c.RequestStateChange(ctx, vmName, RequestedStatePaused)
}

// ResumeVM は一時停止中の VM を再開する。Paused → Enabled の遷移を要求する。
func (c *Client) ResumeVM(ctx context.Context, vmName string) (string, error) {
	return c.RequestStateChange(ctx, vmName, RequestedStateEnabled)
}

// SaveVM は VM の状態を保存して停止する。RequestStateChange(Saved=32769) のショートカット。
func (c *Client) SaveVM(ctx context.Context, vmName string) (string, error) {
	return c.RequestStateChange(ctx, vmName, RequestedStateSaved)
}
