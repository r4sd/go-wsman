package wsman

import (
	"fmt"
	"net/http"
)

// Client は WS-Man クライアント
type Client struct {
	endpoint  string
	transport *HTTPTransport
}

// ClientOption はクライアント構築時のオプション
type ClientOption func(*Client)

// NewClient は新しい WS-Man クライアントを作成する
func NewClient(endpoint string, opts ...ClientOption) *Client {
	c := &Client{
		endpoint: endpoint,
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.transport == nil {
		c.transport = NewHTTPTransport(endpoint, nil)
	}

	return c
}

// WithHTTPClient はカスタム HTTP クライアントを設定する
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.transport = NewHTTPTransport(c.endpoint, httpClient)
	}
}

// WithNTLM は NTLM 認証を設定する（将来実装）
func WithNTLM(username, password string) ClientOption {
	return func(c *Client) {
		// TODO: NTLM 認証トランスポートの実装
		_ = username
		_ = password
	}
}

// Get は WS-Transfer Get 操作を実行する
func (c *Client) Get(resourceURI string, selectors ...Selector) (*GetResponse, error) {
	reqData, err := BuildGetRequest(resourceURI, c.endpoint, selectors...)
	if err != nil {
		return nil, fmt.Errorf("Get リクエストの構築に失敗: %w", err)
	}

	respData, err := c.transport.Send(reqData)
	if err != nil {
		return nil, fmt.Errorf("Get リクエストの送信に失敗: %w", err)
	}

	return ParseGetResponse(respData)
}

// Enumerate は WS-Enumeration 操作を実行し、全インスタンスを返す。
// Enumerate → Pull → EndOfSequence のサイクルを自動的に回す。
func (c *Client) Enumerate(resourceURI string) ([]*Instance, error) {
	// Step 1: Enumerate リクエスト
	enumReqData, err := BuildEnumerateRequest(resourceURI, c.endpoint)
	if err != nil {
		return nil, fmt.Errorf("Enumerate リクエストの構築に失敗: %w", err)
	}

	enumRespData, err := c.transport.Send(enumReqData)
	if err != nil {
		return nil, fmt.Errorf("Enumerate リクエストの送信に失敗: %w", err)
	}

	ctx, err := ParseEnumerateResponse(enumRespData)
	if err != nil {
		return nil, err
	}

	// Step 2: Pull ループ
	var allInstances []*Instance

	for {
		pullReqData, err := BuildPullRequest(resourceURI, c.endpoint, ctx)
		if err != nil {
			return nil, fmt.Errorf("Pull リクエストの構築に失敗: %w", err)
		}

		pullRespData, err := c.transport.Send(pullReqData)
		if err != nil {
			return nil, fmt.Errorf("Pull リクエストの送信に失敗: %w", err)
		}

		pullResp, err := ParsePullResponse(pullRespData)
		if err != nil {
			return nil, err
		}

		allInstances = append(allInstances, pullResp.Items...)

		if pullResp.EndOfSequence {
			break
		}

		ctx = pullResp.EnumerationContext
	}

	return allInstances, nil
}
