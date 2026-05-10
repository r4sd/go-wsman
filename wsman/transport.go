package wsman

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"time"
)

// optimizedTransport は WS-Man に適したデフォルト設定の http.Transport を返す。
// WS-Man は単一ホストに集中してリクエストするため、Go デフォルトの MaxIdleConnsPerHost=2 では不十分。
func optimizedTransport(tlsConfig *tls.Config) *http.Transport {
	return &http.Transport{
		TLSClientConfig:     tlsConfig,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
}

// HTTPTransport は WS-Man SOAP メッセージの HTTP トランスポート
type HTTPTransport struct {
	endpoint       string
	httpClient     *http.Client
	username       string
	password       string
	maxRetries     int
	retryBaseDelay time.Duration
}

// SetCredentials は NTLM/Basic 認証用の資格情報を設定する。
// go-ntlmssp の Negotiator は BasicAuth から資格情報を取得して NTLM ハンドシェイクに使用する。
func (t *HTTPTransport) SetCredentials(username, password string) {
	t.username = username
	t.password = password
}

// NewHTTPTransport は新しい HTTPTransport を作成する。
//
// httpClient が nil の場合、TLS 証明書検証を有効にしたデフォルトクライアントを使用する。
// 自己署名証明書を許容する場合は Client 構築時に WithInsecureSkipVerify() を併用する
// (HTTPTransport 単体での insecure オプションは提供しない)。
func NewHTTPTransport(endpoint string, httpClient *http.Client) *HTTPTransport {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 60 * time.Second,
			Transport: optimizedTransport(&tls.Config{
				MinVersion: tls.VersionTLS12,
			}),
		}
	}

	return &HTTPTransport{
		endpoint:   endpoint,
		httpClient: httpClient,
	}
}

// Send は SOAP リクエストを送信し、レスポンスボディを返す。
// HTTP エラーの場合はエラーを返すが、SOAP Fault を含む HTTP 500 レスポンスは
// ボディデータとして返す（Fault パースは呼び出し側の責任）。
// maxRetries > 0 の場合、接続エラーに対して指数バックオフでリトライする。
// HTTP 4xx/5xx やレスポンスボディ付きのエラーはリトライしない。
func (t *HTTPTransport) Send(ctx context.Context, requestData []byte) ([]byte, error) {
	var lastErr error

	maxAttempts := 1 + t.maxRetries
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := t.retryBaseDelay * (1 << (attempt - 1))
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("context canceled during retry: %w", ctx.Err())
			case <-time.After(delay):
			}
		}

		body, err := t.sendOnce(ctx, requestData)
		if err == nil {
			return body, nil
		}

		lastErr = err

		// リトライ対象外のエラーは即座に返す
		if !isRetryableError(err) {
			return nil, err
		}
	}

	return nil, lastErr
}

// sendOnce は単一の HTTP リクエストを送信する。
func (t *HTTPTransport) sendOnce(ctx context.Context, requestData []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(requestData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/soap+xml;charset=UTF-8")

	if t.username != "" {
		req.SetBasicAuth(t.username, t.password)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read HTTP response: %w", err)
	}

	// SOAP Fault を含む 500 レスポンスはデータとして返す
	if resp.StatusCode >= 400 && len(body) > 0 {
		return body, nil
	}

	if resp.StatusCode >= 400 {
		return nil, &httpStatusError{statusCode: resp.StatusCode}
	}

	return body, nil
}

// httpStatusError はレスポンスボディなしの HTTP エラー（リトライ対象外）
type httpStatusError struct {
	statusCode int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("HTTP error: status code %d", e.statusCode)
}

// isRetryableError は接続エラー等のリトライ可能なエラーかどうかを判定する。
// HTTP ステータスエラー（4xx/5xx）はリトライしない。
func isRetryableError(err error) bool {
	// HTTP ステータスエラーはリトライしない
	if _, ok := err.(*httpStatusError); ok {
		return false
	}
	// それ以外の transport レベルのエラー（接続断、DNS、タイムアウト等）はリトライ対象
	return true
}
