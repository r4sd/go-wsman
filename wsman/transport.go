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

// HTTPTransport は WS-Man SOAP メッセージの HTTP トランスポート
type HTTPTransport struct {
	endpoint   string
	httpClient *http.Client
}

// NewHTTPTransport は新しい HTTPTransport を作成する。
// httpClient が nil の場合、TLS 証明書検証をスキップするデフォルトクライアントを使用する。
func NewHTTPTransport(endpoint string, httpClient *http.Client) *HTTPTransport {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //#nosec G402 -- WinRM は自己署名証明書が一般的
				},
			},
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
func (t *HTTPTransport) Send(ctx context.Context, requestData []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(requestData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/soap+xml;charset=UTF-8")

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
		return nil, fmt.Errorf("HTTP error: status code %d", resp.StatusCode)
	}

	return body, nil
}
