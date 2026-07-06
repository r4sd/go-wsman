package hyperv

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newJobServer は Get リクエストごとに responses を順に返すテストサーバーを作る。
// responses が尽きたら最後の応答を返し続ける (ポーリングの「ずっと実行中」を表現)。
func newJobServer(t *testing.T, responses ...string) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := calls
		if idx >= len(responses) {
			idx = len(responses) - 1
		}
		calls++
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(responses[idx]))
	}))
	return server, &calls
}

// TestClient_WaitForJob_Completed は JobState=7 (Completed) で nil を返すことを検証する。
func TestClient_WaitForJob_Completed(t *testing.T) {
	server, _ := newJobServer(t, loadGolden(t, "get_response_concretejob_completed.xml"))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.WaitForJob(context.Background(), "9C7D3E22-AAAA-BBBB-CCCC-111122223333"); err != nil {
		t.Fatalf("WaitForJob: got %v, want nil", err)
	}
}

// TestClient_WaitForJob_RunningThenCompleted は実行中→完了へ遷移する Job を
// ポーリングして最終的に nil を返すことを検証する。
func TestClient_WaitForJob_RunningThenCompleted(t *testing.T) {
	server, calls := newJobServer(t,
		loadGolden(t, "get_response_concretejob_running.xml"),
		loadGolden(t, "get_response_concretejob_completed.xml"),
	)
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.WaitForJob(context.Background(), "9C7D3E22-AAAA-BBBB-CCCC-111122223333",
		WithPollInterval(1*time.Millisecond)); err != nil {
		t.Fatalf("WaitForJob: got %v, want nil", err)
	}
	if *calls < 2 {
		t.Errorf("expected at least 2 polls (running then completed), got %d", *calls)
	}
}

// TestClient_WaitForJob_Exception は JobState=10 (Exception) でエラーを返し、
// ErrorDescription がメッセージに含まれることを検証する。
func TestClient_WaitForJob_Exception(t *testing.T) {
	server, _ := newJobServer(t, loadGolden(t, "get_response_concretejob_exception.xml"))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = client.WaitForJob(context.Background(), "9C7D3E22-AAAA-BBBB-CCCC-111122223333")
	if err == nil {
		t.Fatal("expected error for failed job, got nil")
	}
	if !strings.Contains(err.Error(), "The operation failed.") {
		t.Errorf("error should contain ErrorDescription; got %v", err)
	}
}

// TestClient_WaitForJob_EmptyJobRef は jobRef が空 (同期完了) の場合に
// 通信せず nil を返すことを検証する。
func TestClient_WaitForJob_EmptyJobRef(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.WaitForJob(context.Background(), ""); err != nil {
		t.Fatalf("WaitForJob(\"\"): got %v, want nil", err)
	}
	if called {
		t.Error("WaitForJob with empty jobRef should not make any HTTP call")
	}
}

// TestClient_WaitForJob_Timeout は実行中のまま完了しない Job がタイムアウトで
// エラー (DeadlineExceeded) を返すことを検証する。
func TestClient_WaitForJob_Timeout(t *testing.T) {
	server, _ := newJobServer(t, loadGolden(t, "get_response_concretejob_running.xml"))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = client.WaitForJob(context.Background(), "9C7D3E22-AAAA-BBBB-CCCC-111122223333",
		WithPollInterval(1*time.Millisecond), WithJobTimeout(20*time.Millisecond))
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected errors.Is(err, context.DeadlineExceeded), got %v", err)
	}
}

// TestClient_WaitForJob_ContextCancel は ctx キャンセルでエラー (Canceled) を
// 返すことを検証する。
func TestClient_WaitForJob_ContextCancel(t *testing.T) {
	server, _ := newJobServer(t, loadGolden(t, "get_response_concretejob_running.xml"))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	err = client.WaitForJob(ctx, "9C7D3E22-AAAA-BBBB-CCCC-111122223333",
		WithPollInterval(1*time.Millisecond), WithJobTimeout(5*time.Second))
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected errors.Is(err, context.Canceled), got %v", err)
	}
}

// newFlakyJobServer は最初の failCount 回を HTTP 500 (過渡的エラー) で返し、
// 以降は response を返すテストサーバーを作る。
func newFlakyJobServer(t *testing.T, failCount int, response string) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls
		calls++
		if n < failCount {
			http.Error(w, "transient", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(response))
	}))
	return server, &calls
}

// TestClient_WaitForJob_TransientErrorThenCompleted は poll 中の過渡的エラー(WinRM 断相当)を
// maxPollErrors まで許容し、回復後に完了を検出できることを検証する (#92)。
func TestClient_WaitForJob_TransientErrorThenCompleted(t *testing.T) {
	// 最初の 3 回失敗 → 4 回目で完了。デフォルト maxPollErrors=5 の範囲内なので成功するはず。
	server, calls := newFlakyJobServer(t, 3, loadGolden(t, "get_response_concretejob_completed.xml"))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.WaitForJob(context.Background(), "9C7D3E22-AAAA-BBBB-CCCC-111122223333",
		WithPollInterval(1*time.Millisecond)); err != nil {
		t.Fatalf("WaitForJob: 過渡的エラーを吸収して成功するはず, got %v", err)
	}
	if *calls < 4 {
		t.Errorf("expected at least 4 polls (3 fail + 1 success), got %d", *calls)
	}
}

// TestClient_WaitForJob_ExhaustsPollErrors は連続失敗が maxPollErrors を超えたら
// 諦めてエラーを返すことを検証する (#92)。
func TestClient_WaitForJob_ExhaustsPollErrors(t *testing.T) {
	// 常に失敗。maxPollErrors=2 なので 3 回目の失敗で諦める。
	server, calls := newFlakyJobServer(t, 1000, "")
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = client.WaitForJob(context.Background(), "9C7D3E22-AAAA-BBBB-CCCC-111122223333",
		WithPollInterval(1*time.Millisecond), WithMaxPollErrors(2), WithJobTimeout(5*time.Second))
	if err == nil {
		t.Fatal("expected error after exhausting poll retries, got nil")
	}
	if !strings.Contains(err.Error(), "連続") {
		t.Errorf("expected '連続で失敗' error, got %v", err)
	}
	// maxPollErrors=2 → 3 回失敗で諦め (consecErrors > 2)。過剰にリトライしていないこと。
	if *calls > 4 {
		t.Errorf("expected to give up around 3 polls, got %d", *calls)
	}
}

// TestClient_WaitForJob_ZeroMaxPollErrors は WithMaxPollErrors(0) で従来挙動
// (1 回の失敗で即座に諦める) になることを検証する。
func TestClient_WaitForJob_ZeroMaxPollErrors(t *testing.T) {
	server, calls := newFlakyJobServer(t, 1000, "")
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = client.WaitForJob(context.Background(), "9C7D3E22-AAAA-BBBB-CCCC-111122223333",
		WithPollInterval(1*time.Millisecond), WithMaxPollErrors(0), WithJobTimeout(5*time.Second))
	if err == nil {
		t.Fatal("expected immediate error with maxPollErrors=0, got nil")
	}
	if *calls != 1 {
		t.Errorf("expected exactly 1 poll (no retry), got %d", *calls)
	}
}
