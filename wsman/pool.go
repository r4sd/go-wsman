package wsman

import (
	"context"
	"fmt"
)

// pooledTransport は N 個の独立した HTTPTransport (各々が別々の *http.Client / コネクション
// プールを持つ) の間でリクエストを振り分ける。
//
// NTLM はコネクション単位でハンドシェイク状態を保持するプロトコルで、negotiate→challenge→
// authenticate の 3 ステップが同一 TCP コネクション上で完結する必要がある
// (github.com/Azure/go-ntlmssp の Negotiator.RoundTrip 参照)。単一の *http.Transport
// (コネクションプール共有) に対して複数 goroutine が同時にリクエストすると、ある
// goroutine のハンドシェイク中に別の goroutine のリクエストが同じコネクションへ割り込み、
// サーバー側の NTLM 状態機械が混乱して 401 になりうる (#117、実機で
// Terraform 既定 parallelism=10 の並行 GetVhd 7 件が全滅、parallelism=1 なら成功、と確認)。
//
// pooledTransport は各スロットに完全に独立した HTTPTransport (=別々のコネクションプール) を
// 割り当てることで、この干渉を構造的に防ぐ。1 スロットは 1 度に 1 リクエストしか使わない
// (チャネルによる排他)。
type pooledTransport struct {
	slots chan *HTTPTransport
}

// Send はプールから空きスロットを 1 つ借りてリクエストを送信し、完了後に返却する。
// 全スロットが使用中なら ctx が完了するまで待つ。
func (p *pooledTransport) Send(ctx context.Context, requestData []byte) ([]byte, error) {
	select {
	case t := <-p.slots:
		defer func() { p.slots <- t }()
		return t.Send(ctx, requestData)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// NewPooledClient は size 個の完全に独立した認証コンテキスト (別々のコネクションプール) を
// 持つ Client を構築する。同じ opts で NewClient を size 回呼び、各々の transport を集めて
// プール化する (認証方式・TLS 設定等は既存の NewClient の適用ロジックをそのまま再利用する
// ため、opts の組み合わせに関する既存の挙動を変えない)。
//
// 戻り値の Client は通常の Client と同じ API で使えるが、内部で size 個のコネクション
// プールを使い分けるため、同時実行数が size を超えても NTLM ハンドシェイクが交錯しない
// (#117)。同時実行数の実質的な上限は size になる (それを超える分は空きスロット待ちでブロック)。
func NewPooledClient(size int, endpoint string, opts ...ClientOption) (*Client, error) {
	if size <= 0 {
		return nil, fmt.Errorf("NewPooledClient: size must be > 0, got %d", size)
	}

	slots := make(chan *HTTPTransport, size)
	for i := 0; i < size; i++ {
		c, err := NewClient(endpoint, opts...)
		if err != nil {
			return nil, fmt.Errorf("NewPooledClient: client %d: %w", i, err)
		}
		slots <- c.transport
	}

	return &Client{
		endpoint: endpoint,
		pool:     &pooledTransport{slots: slots},
	}, nil
}
