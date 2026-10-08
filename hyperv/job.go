package hyperv

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/r4sd/go-wsman/wsman"
)

const msvmConcreteJobURI = nsVirtV2 + "/Msvm_ConcreteJob"

// デフォルトのポーリング間隔とタイムアウト。WaitOption で上書きできる。
const (
	defaultJobPollInterval = 500 * time.Millisecond
	defaultJobTimeout      = 5 * time.Minute
	// defaultMaxJobPollErrors は getJob の連続失敗をどこまで許容するか。poll 中の一時的な
	// WinRM 断・過渡的 Fault で即座に待機を諦めると、非同期操作(特に detach の 2 段削除や
	// 連鎖ジョブ)が中途半端な state を残す。連続失敗がこの回数を超えたら本当の失敗として扱う。
	// 1 回でも成功すればカウンタはリセットするので、散発的な blip は吸収する。
	defaultMaxJobPollErrors = 5
)

// waitConfig は WaitForJob の挙動設定。
type waitConfig struct {
	pollInterval  time.Duration
	timeout       time.Duration
	maxPollErrors int
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

// WithMaxPollErrors は Job 状態取得(getJob)の連続失敗を許容する回数を設定する。
// 0 以下を渡すと過渡的エラーを一切許容しない(従来挙動: 1 回の失敗で即座に諦める)。
func WithMaxPollErrors(n int) WaitOption {
	return func(c *waitConfig) { c.maxPollErrors = n }
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

	cfg := waitConfig{pollInterval: defaultJobPollInterval, timeout: defaultJobTimeout, maxPollErrors: defaultMaxJobPollErrors}
	for _, o := range opts {
		o(&cfg)
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	ticker := time.NewTicker(cfg.pollInterval)
	defer ticker.Stop()

	consecErrors := 0
	for {
		job, err := c.getJob(ctx, epr.ResourceURI, instanceID)
		if err != nil {
			// poll 中の過渡的エラー(WinRM 断等)は maxPollErrors まで許容し、次の tick で再試行する。
			// ctx キャンセル/タイムアウトは過渡的でないので即座に返す。
			if ctx.Err() != nil {
				return fmt.Errorf("WaitForJobEPR %s: %w", instanceID, ctx.Err())
			}
			consecErrors++
			if consecErrors > cfg.maxPollErrors {
				return fmt.Errorf("WaitForJobEPR %s: %d 回連続で job 状態取得に失敗: %w", instanceID, consecErrors, err)
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("WaitForJobEPR %s: %w", instanceID, ctx.Err())
			case <-ticker.C:
			}
			continue
		}
		consecErrors = 0 // 成功したら連続失敗カウンタをリセット(散発 blip を吸収)
		switch job.JobState {
		case JobStateCompleted:
			return nil
		case JobStateTerminated, JobStateKilled, JobStateException:
			return fmt.Errorf("WaitForJobEPR %s%s: job failed (JobState=%s, ErrorCode=%d): %s",
				instanceID, jobDescription(epr.ResourceURI, job),
				jobStateName(job.JobState), job.ErrorCode, job.ErrorDescription)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("WaitForJobEPR %s: %w", instanceID, ctx.Err())
		case <-ticker.C:
		}
	}
}

// getJob は指定 ResourceURI + InstanceID の Job を取得する。
//
// resourceURI が Msvm_StorageJob(ImageManagementService 経路)でも戻り値を
// Msvm_ConcreteJob struct で受けられるのは、両者が継承関係ではなく、どちらも
// CIM_ConcreteJob を親に持つ兄弟クラスで、この struct が読むプロパティと JobState の
// enum が名前・型レベルで一致するため。
//
// MOF 一次資料(2026-09-27 時点、いずれも `class Msvm_* : CIM_ConcreteJob`):
//   - https://learn.microsoft.com/en-us/windows/win32/hyperv_v2/msvm-storagejob
//   - https://learn.microsoft.com/en-us/windows/win32/hyperv_v2/msvm-concretejob
//
// 一致の中身は testdata/mof/msvm_storagejob.txt に写してあり、
// TestCIMCompliance_ConcreteJob_AgainstStorageJobMOF が struct と突合する。
func (c *Client) getJob(ctx context.Context, resourceURI, instanceID string) (*Msvm_ConcreteJob, error) {
	resp, err := c.wsman.Get(ctx, resourceURI,
		wsman.Selector{Name: "InstanceID", Value: instanceID},
	)
	if err != nil {
		return nil, err
	}
	var job Msvm_ConcreteJob
	if err := UnmarshalList(resp.PropertiesList(), &job); err != nil {
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

// jobDescription は失敗メッセージに添える「どの操作の Job か」を組み立てる。
//
// InstanceID と ErrorCode だけでは、落ちたのが VHD の作成なのかマージなのか
// スナップショットなのか分からない (#189)。実機の応答は表示名と JobType を
// 返しているので、それを人が読む形で添える。
//
// 戻り値は先頭に区切りを含む (呼び出し側で "%s%s" と連結する前提)。
// 添える情報が何も無ければ空文字列を返す。
//
// 🔴 **表示名はロケール依存**なので、分岐には使わない。
// 🔴 **JobType の列挙はクラスごとに別物**なので、名前に展開せず
// 数値とクラス名を併記する。クラス名は ResourceURI の末尾から取る。
func jobDescription(resourceURI string, job *Msvm_ConcreteJob) string {
	// ElementName と Name は実機では同じ値だったが、MOF 上は別プロパティ
	// (ElementName=表示名 / Name=ラベル)。空でない方を使う。
	label := job.ElementName
	if label == "" {
		label = job.Name
	}

	var parts []string
	if label != "" {
		parts = append(parts, label)
	}
	if job.JobType != 0 {
		// クラス名が取れないときは JobType だけでは意味が決まらないので、
		// その旨が分かるように "JobType=N" のまま出す。
		if cls := cimClassFromURI(resourceURI); cls != "" {
			parts = append(parts, fmt.Sprintf("JobType=%d of %s", job.JobType, cls))
		} else {
			parts = append(parts, fmt.Sprintf("JobType=%d", job.JobType))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, ", ") + "]"
}

// cimClassFromURI は ResourceURI の末尾のクラス名を返す。取れなければ空文字列。
//
// JobType の数値は**クラスが決まらないと意味が決まらない**ので、
// 数値を出すときは必ずこれを添える。
func cimClassFromURI(resourceURI string) string {
	if i := strings.LastIndex(resourceURI, "/"); i >= 0 && i+1 < len(resourceURI) {
		return resourceURI[i+1:]
	}
	return ""
}
