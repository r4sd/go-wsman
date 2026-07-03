package hyperv

import (
	"context"
	"fmt"
	"time"

	"github.com/r4sd/go-wsman/wsman"
)

const msvmConcreteJobURI = nsVirtV2 + "/Msvm_ConcreteJob"

// デフォルトのポーリング間隔とタイムアウト。WaitOption で上書きできる。
const (
	defaultJobPollInterval = 500 * time.Millisecond
	defaultJobTimeout      = 5 * time.Minute
)

// waitConfig は WaitForJob の挙動設定。
type waitConfig struct {
	pollInterval time.Duration
	timeout      time.Duration
}

// WaitOption は WaitForJob のオプション。
type WaitOption func(*waitConfig)

// WithPollInterval は Job 状態のポーリング間隔を設定する。
func WithPollInterval(d time.Duration) WaitOption {
	return func(c *waitConfig) { c.pollInterval = d }
}

// WithJobTimeout は Job 完了待ちのタイムアウトを設定する。
func WithJobTimeout(d time.Duration) WaitOption {
	return func(c *waitConfig) { c.timeout = d }
}

// WaitForJob は非同期 CIM 操作 (ReturnValue=4096) が返した Job の完了を待つ。
//
// jobRef は Msvm_ConcreteJob の InstanceID (各メソッドが resp.Property("Job") で
// 返す値)。空文字列の場合は同期完了 = 待つものなしとして即 nil を返すため、
// 呼び出し側は ReturnValue の分岐なしに WaitForJob を呼べる。
//
// JobState が Completed(7) になれば nil、Terminated(8)/Killed(9)/Exception(10) なら
// ErrorCode / ErrorDescription を含むエラーを返す。タイムアウト・ctx キャンセル時は
// その理由を返す。
func (c *Client) WaitForJob(ctx context.Context, jobRef string, opts ...WaitOption) error {
	if jobRef == "" {
		return nil // 同期完了 = 待つものなし
	}
	// 裸 InstanceID は Msvm_ConcreteJob 前提。EPR に組み立てて共通実装 (WaitForJobEPR) に委譲する。
	return c.WaitForJobEPR(ctx, &wsman.EndpointReference{
		ResourceURI: msvmConcreteJobURI,
		Selectors:   map[string]string{"InstanceID": jobRef},
	}, opts...)
}

// WaitForJobEPR は非同期 CIM 操作が返した Job の EPR を使って完了を待つ。
//
// WaitForJob(裸 InstanceID) が Msvm_ConcreteJob を前提とするのと違い、EPR の ResourceURI を
// 使うため Job の実クラスがメソッドで異なっても正しく Get できる (ImageManagementService は
// Msvm_StorageJob、VSMS は Msvm_ConcreteJob)。同期完了 (epr==nil) は待つものなしで成功。
func (c *Client) WaitForJobEPR(ctx context.Context, epr *wsman.EndpointReference, opts ...WaitOption) error {
	if epr == nil {
		return nil
	}
	instanceID := epr.Selectors["InstanceID"]
	if instanceID == "" {
		return fmt.Errorf("WaitForJobEPR: EPR に InstanceID セレクタが無い (%s)", epr.ResourceURI)
	}

	cfg := waitConfig{pollInterval: defaultJobPollInterval, timeout: defaultJobTimeout}
	for _, o := range opts {
		o(&cfg)
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	ticker := time.NewTicker(cfg.pollInterval)
	defer ticker.Stop()

	for {
		job, err := c.getJob(ctx, epr.ResourceURI, instanceID)
		if err != nil {
			return fmt.Errorf("WaitForJobEPR %s: %w", instanceID, err)
		}
		switch job.JobState {
		case JobStateCompleted:
			return nil
		case JobStateTerminated, JobStateKilled, JobStateException:
			return fmt.Errorf("WaitForJobEPR %s: job failed (JobState=%s, ErrorCode=%d): %s",
				instanceID, jobStateName(job.JobState), job.ErrorCode, job.ErrorDescription)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("WaitForJobEPR %s: %w", instanceID, ctx.Err())
		case <-ticker.C:
		}
	}
}

// getJob は指定 ResourceURI + InstanceID の Job を取得する。Msvm_StorageJob は JobState 等を
// Msvm_ConcreteJob(CIM_ConcreteJob)から継承しているため、同じ struct/Unmarshal で扱える。
func (c *Client) getJob(ctx context.Context, resourceURI, instanceID string) (*Msvm_ConcreteJob, error) {
	resp, err := c.wsman.Get(ctx, resourceURI,
		wsman.Selector{Name: "InstanceID", Value: instanceID},
	)
	if err != nil {
		return nil, err
	}
	var job Msvm_ConcreteJob
	if err := Unmarshal(resp.Properties(), &job); err != nil {
		return nil, fmt.Errorf("failed to unmarshal job: %w", err)
	}
	return &job, nil
}

// jobStateName は JobState の数値を人間可読な名前に変換する (エラーメッセージ用)。
func jobStateName(s uint16) string {
	switch s {
	case JobStateNew:
		return "New"
	case JobStateStarting:
		return "Starting"
	case JobStateRunning:
		return "Running"
	case JobStateSuspended:
		return "Suspended"
	case JobStateShuttingDown:
		return "ShuttingDown"
	case JobStateCompleted:
		return "Completed"
	case JobStateTerminated:
		return "Terminated"
	case JobStateKilled:
		return "Killed"
	case JobStateException:
		return "Exception"
	case JobStateService:
		return "Service"
	default:
		return fmt.Sprintf("Unknown(%d)", s)
	}
}
