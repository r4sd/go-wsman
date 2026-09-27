package hyperv

import (
	"context"
	"fmt"
	"strconv"

	"github.com/r4sd/go-wsman/wsman"
)

// RequestedState 定数 (Msvm_ComputerSystem.RequestStateChange の RequestedState)
//
// 🔴 以前ここには「Hyper-V は CIM 標準値に加えて拡張値 (32768/32769) も受け付ける」と
// 書いてあったが**誤り**。v2 の Msvm_ComputerSystem に 32768/32769 を渡すと
// ReturnValue=32775 (invalid state for this operation) で拒否され、状態は変わらない
// (2026-09-27 実機 1 台で確認。Running 安定後に 6 回リトライしても全て 32775)。
//
// 32768/32769 は Hyper-V v1 の値で、DMTF の CIM 標準には無い。紛らわしいのは
// **MS の v2 Msvm_ComputerSystem ページにも v1 由来の記述が残っている**こと
// (HealthState 節の「EnabledState is set to 32768 (Paused)」、OperationalStatus 節の
// 「32769 (Suspended) or 32768 (Paused)」)。同じページの EnabledState 表は 0〜10 しか
// 載せていない。誤りの元は思い込みというより、この公式ドキュメント内の食い違い。
//
// Paused/Saved は Quiesce(9) / Offline(6) で要求する。結果として EnabledState も
// 同じ 9 / 6 になるので、入力値と現在値の数値が一致する (#102)。
//
// 各値の裏取り状況:
//   - 2 Enabled / 3 Disabled / 9 Quiesce / 6 Offline: 2026-09-27 実機確認
//   - 4 ShutDown: MOF は "Valid in version 1 (V1) of Hyper-V only" と書くが、
//     **v2 実機で成功している** (provider #71 の graceful shutdown 実機テスト)。
//     MOF の記述を鵜呑みにしないこと
//   - 10 Reboot: 未検証。MOF の説明は "State transition from Off or Saved to Running" で、
//     「強制再起動」という理解と食い違う
//   - 11 Reset: 未検証
const (
	RequestedStateEnabled  uint16 = 2  // Start: VM を起動
	RequestedStateDisabled uint16 = 3  // TurnOff: 強制電源断 (Hyper-V のシャットダウンではない)
	RequestedStateShutDown uint16 = 4  // Shutdown: ゲスト OS のシャットダウンを要求 (Integration Services 必須。provider #71 で実機確認済み)
	RequestedStateSaved    uint16 = 6  // Offline: 状態を保存して停止 (Save-VM)
	RequestedStatePaused   uint16 = 9  // Quiesce: 一時停止 (Suspend-VM)
	RequestedStateReboot   uint16 = 10 // Reboot (未検証。MOF: "Off or Saved から Running への遷移")
	RequestedStateReset    uint16 = 11 // Reset (未検証)
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

// PauseVM は VM を一時停止する。RequestStateChange(RequestedStatePaused) のショートカット。
func (c *Client) PauseVM(ctx context.Context, vmName string) (string, error) {
	return c.RequestStateChange(ctx, vmName, RequestedStatePaused)
}

// ResumeVM は一時停止中の VM を再開する。Paused → Enabled の遷移を要求する。
func (c *Client) ResumeVM(ctx context.Context, vmName string) (string, error) {
	return c.RequestStateChange(ctx, vmName, RequestedStateEnabled)
}

// SaveVM は VM の状態を保存して停止する。RequestStateChange(RequestedStateSaved) のショートカット。
func (c *Client) SaveVM(ctx context.Context, vmName string) (string, error) {
	return c.RequestStateChange(ctx, vmName, RequestedStateSaved)
}
