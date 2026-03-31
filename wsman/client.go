package wsman

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/Azure/go-ntlmssp"
)

// DefaultMaxPullIterations は Enumerate の Pull ループの最大反復回数。
// 無限ループ防止のための安全策。
const DefaultMaxPullIterations = 1000

// Client は WS-Man クライアント
type Client struct {
	endpoint  string
	transport *HTTPTransport
}

// ClientOption はクライアント構築時のオプション
type ClientOption func(*Client)

// NewClient は新しい WS-Man クライアントを作成する。
// endpoint が有効な URL でない場合はエラーを返す。
func NewClient(endpoint string, opts ...ClientOption) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("invalid endpoint URL scheme %q: must be http or https", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("invalid endpoint URL: missing host")
	}

	c := &Client{
		endpoint: endpoint,
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.transport == nil {
		c.transport = NewHTTPTransport(endpoint, nil)
	}

	return c, nil
}

// WithHTTPClient はカスタム HTTP クライアントを設定する
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.transport = NewHTTPTransport(c.endpoint, httpClient)
	}
}

// WithNTLM は NTLM 認証を設定する。
// username は "DOMAIN\\user" 形式または単純なユーザー名を受け付ける。
// go-ntlmssp の Negotiator が NTLM ハンドシェイクを透過的に処理する。
func WithNTLM(username, password string) ClientOption {
	return func(c *Client) {
		c.transport = newNTLMTransport(c.endpoint, username, password)
	}
}

// WithNTLMAuth は NTLM 認証をドメイン指定付きで設定する。
// domain が空でない場合、"DOMAIN\\username" 形式でハンドシェイクに使用する。
func WithNTLMAuth(domain, username, password string) ClientOption {
	return func(c *Client) {
		user := username
		if domain != "" {
			user = domain + `\` + username
		}
		c.transport = newNTLMTransport(c.endpoint, user, password)
	}
}

// newNTLMTransport は NTLM 認証付きの HTTPTransport を作成する。
func newNTLMTransport(endpoint, username, password string) *HTTPTransport {
	baseTransport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //#nosec G402 -- WinRM は自己署名証明書が一般的
		},
	}

	httpClient := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &ntlmssp.Negotiator{
			RoundTripper: baseTransport,
		},
	}

	t := NewHTTPTransport(endpoint, httpClient)
	t.SetCredentials(username, password)
	return t
}

// Get は WS-Transfer Get 操作を実行する
func (c *Client) Get(ctx context.Context, resourceURI string, selectors ...Selector) (*GetResponse, error) {
	reqData, err := BuildGetRequest(resourceURI, c.endpoint, selectors...)
	if err != nil {
		return nil, fmt.Errorf("failed to build Get request: %w", err)
	}

	respData, err := c.transport.Send(ctx, reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to send Get request: %w", err)
	}

	return ParseGetResponse(respData)
}

// Enumerate は WS-Enumeration 操作を実行し、全インスタンスを返す。
// Enumerate → Pull → EndOfSequence のサイクルを自動的に回す。
func (c *Client) Enumerate(ctx context.Context, resourceURI string) ([]*Instance, error) {
	// Step 1: Enumerate リクエスト
	enumReqData, err := BuildEnumerateRequest(resourceURI, c.endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to build Enumerate request: %w", err)
	}

	enumRespData, err := c.transport.Send(ctx, enumReqData)
	if err != nil {
		return nil, fmt.Errorf("failed to send Enumerate request: %w", err)
	}

	enumCtx, err := ParseEnumerateResponse(enumRespData)
	if err != nil {
		return nil, err
	}

	// Step 2: Pull ループ
	var allInstances []*Instance

	for i := 0; i < DefaultMaxPullIterations; i++ {
		pullReqData, err := BuildPullRequest(resourceURI, c.endpoint, enumCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to build Pull request: %w", err)
		}

		pullRespData, err := c.transport.Send(ctx, pullReqData)
		if err != nil {
			return nil, fmt.Errorf("failed to send Pull request: %w", err)
		}

		pullResp, err := ParsePullResponse(pullRespData)
		if err != nil {
			return nil, err
		}

		allInstances = append(allInstances, pullResp.Items...)

		if pullResp.EndOfSequence {
			return allInstances, nil
		}

		enumCtx = pullResp.EnumerationContext
	}

	return nil, fmt.Errorf("exceeded maximum Pull iterations (%d)", DefaultMaxPullIterations)
}
