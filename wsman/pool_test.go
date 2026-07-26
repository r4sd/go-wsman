package wsman

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewPooledClient_InvalidSize は size<=0 がエラーになることを検証する。
func TestNewPooledClient_InvalidSize(t *testing.T) {
	if _, err := NewPooledClient(0, "https://host:5986/wsman"); err == nil {
		t.Error("size=0 はエラーになるべき")
	}
	if _, err := NewPooledClient(-1, "https://host:5986/wsman"); err == nil {
		t.Error("size=-1 はエラーになるべき")
	}
}

// TestNewPooledClient_Functional はプール経由の Get がモックサーバーに対して正しく動作することを
// 検証する (機能面の確認、通常の NewClient と同じ結果になるべき)。
func TestNewPooledClient_Functional(t *testing.T) {
	responseXML := loadGolden(t, "get_response_computersystem.xml")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
		_, _ = w.Write(responseXML)
	}))
	defer server.Close()

	client, err := NewPooledClient(3, server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewPooledClient: %v", err)
	}

	resp, err := client.Get(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem")
	if err != nil {
		t.Fatalf("Client.Get: %v", err)
	}
	if resp == nil {
		t.Fatal("resp is nil")
	}
}

// TestNewPooledClient_LimitsConcurrency は同時実行数がプールサイズを超えないことを検証する。
// N > size 件のリクエストを同時に投げ、サーバー側で観測した同時実行数の最大値が size を
// 超えないことを確認する (#117: NTLM の 3 段ハンドシェイクが別リクエストと交錯しないよう、
// 各プールスロットは 1 度に 1 リクエストしか使わないことの裏付け)。
func TestNewPooledClient_LimitsConcurrency(t *testing.T) {
	const poolSize = 3
	const requestCount = 9

	var current, max int64
	var mu sync.Mutex
	updateMax := func(v int64) {
		mu.Lock()
		defer mu.Unlock()
		if v > max {
			max = v
		}
	}

	responseXML := loadGolden(t, "get_response_computersystem.xml")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt64(&current, 1)
		updateMax(n)
		time.Sleep(20 * time.Millisecond) // 同時実行を観測しやすくする
		atomic.AddInt64(&current, -1)
		w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
		_, _ = w.Write(responseXML)
	}))
	defer server.Close()

	client, err := NewPooledClient(poolSize, server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewPooledClient: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.Get(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem"); err != nil {
				t.Errorf("Client.Get: %v", err)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	got := max
	mu.Unlock()
	if got > poolSize {
		t.Errorf("同時実行数の最大値 = %d, want <= %d (プールサイズを超えている)", got, poolSize)
	}
	if got < 1 {
		t.Error("同時実行が全く観測されなかった (テスト自体が機能していない可能性)")
	}
}

// TestNewPooledClient_ContextCancel は全スロットが埋まっている間に ctx がキャンセルされた場合、
// ブロックせず ctx.Err() を返すことを検証する。
func TestNewPooledClient_ContextCancel(t *testing.T) {
	block := make(chan struct{})
	// defer は LIFO なので server.Close() を先に defer し、close(block) を後に defer する
	// (実行順は close(block) → server.Close() となり、ブロック中のハンドラを先に解放してから
	// Close() する。逆順だと Close() がハンドラ解放を待って 5 秒 stall する)。
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block // テスト終了までレスポンスを返さない
	}))
	defer server.Close()
	defer close(block)

	client, err := NewPooledClient(1, server.URL, WithHTTPClient(server.Client()), WithTimeout(time.Hour))
	if err != nil {
		t.Fatalf("NewPooledClient: %v", err)
	}

	// 1 スロットを埋める (レスポンスが返らないので送信中のまま止まる)。
	go func() {
		_, _ = client.Get(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem")
	}()
	time.Sleep(50 * time.Millisecond) // 上記 goroutine がスロットを借用するのを待つ

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = client.Get(ctx, "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem")
	if err == nil {
		t.Error("空きスロットが無い状態で ctx がタイムアウトしたらエラーになるべき")
	}
}
