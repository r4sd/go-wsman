package wsman

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/Azure/go-ntlmssp"
)

// DefaultMaxPullIterations は Enumerate の Pull ループの最大反復回数。
// 無限ループ防止のための安全策。
const DefaultMaxPullIterations = 1000

// Client は WS-Man クライアント
type Client struct {
	endpoint    string
	transport   *HTTPTransport
	timeout     time.Duration // 0 の場合はデフォルト（60s）を使用、明示的に設定された場合は上書き
	timeoutSet  bool          // WithTimeout が呼ばれたかどうか
	retryConfig *retryConfig  // nil の場合はリトライなし
	optErr      error         // オプション適用時のエラー（遅延チェック用）
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

	if c.optErr != nil {
		return nil, c.optErr
	}

	if c.transport == nil {
		c.transport = NewHTTPTransport(endpoint, nil)
	}

	// WithTimeout が呼ばれた場合、transport 確定後にタイムアウトを適用
	if c.timeoutSet {
		c.transport.httpClient.Timeout = c.timeout
	}

	// WithRetry が呼ばれた場合、transport 確定後にリトライ設定を適用
	if c.retryConfig != nil {
		c.transport.maxRetries = c.retryConfig.maxRetries
		c.transport.retryBaseDelay = c.retryConfig.retryBaseDelay
	}

	return c, nil
}

// WithTimeout は HTTP リクエストのタイムアウトを設定する。
// 0 を指定するとタイムアウト無制限になる。
// WithNTLM や WithCertificate 等の他オプションと組み合わせ可能。
// transport 確定後に適用されるため、オプションの順序に依存しない。
func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) {
		c.timeout = d
		c.timeoutSet = true
	}
}

// WithRetry は接続エラー時のリトライ回数を設定する。
// 指数バックオフ（1s, 2s, 4s, ...）でリトライする。
// HTTP 4xx/5xx や SOAP Fault はリトライしない（接続エラーのみ対象）。
func WithRetry(maxRetries int) ClientOption {
	return func(c *Client) {
		c.retryConfig = &retryConfig{
			maxRetries:     maxRetries,
			retryBaseDelay: 1 * time.Second,
		}
	}
}

// retryConfig はリトライ設定を保持する
type retryConfig struct {
	maxRetries     int
	retryBaseDelay time.Duration
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

// WithCertificate はファイルパスから TLS クライアント証明書認証を設定する。
// certFile と keyFile は PEM エンコードされた証明書と秘密鍵のパス。
func WithCertificate(certFile, keyFile string) ClientOption {
	return func(c *Client) {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			c.optErr = fmt.Errorf("failed to load client certificate: %w", err)
			return
		}
		c.transport = newCertTransport(c.endpoint, &cert, nil)
	}
}

// WithCertificateConfig は tls.Certificate を直接設定する。
// テストやメモリ上の証明書を使用する場合に便利。
func WithCertificateConfig(cert tls.Certificate) ClientOption {
	return func(c *Client) {
		c.transport = newCertTransport(c.endpoint, &cert, nil)
	}
}

// WithCACert はカスタム CA 証明書を信頼リストに追加する。
// 自己署名 CA を使用する場合に WithCertificate / WithCertificateConfig と組み合わせて使用する。
// 既に transport が設定されている場合はその TLS 設定に CA を追加し、
// 未設定の場合は新しい transport を作成する。
func WithCACert(caFile string) ClientOption {
	return func(c *Client) {
		caPEM, err := os.ReadFile(caFile) //#nosec G304 -- CA 証明書パスはユーザー指定のライブラリ API
		if err != nil {
			c.optErr = fmt.Errorf("failed to read CA certificate file: %w", err)
			return
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caPEM) {
			c.optErr = fmt.Errorf("failed to parse CA certificate from %s", caFile)
			return
		}

		if c.transport != nil {
			// 既存の transport の TLS 設定に CA を追加
			addCACertToTransport(c.transport, caPool)
		} else {
			// transport 未設定の場合は CA のみで新規作成
			c.transport = newCertTransport(c.endpoint, nil, caPool)
		}
	}
}

// newCertTransport は TLS クライアント証明書認証付きの HTTPTransport を作成する。
func newCertTransport(endpoint string, cert *tls.Certificate, rootCAs *x509.CertPool) *HTTPTransport {
	tlsConfig := &tls.Config{}

	if cert != nil {
		tlsConfig.Certificates = []tls.Certificate{*cert}
	}
	if rootCAs != nil {
		tlsConfig.RootCAs = rootCAs
	}

	httpClient := &http.Client{
		Timeout:   60 * time.Second,
		Transport: optimizedTransport(tlsConfig),
	}

	return NewHTTPTransport(endpoint, httpClient)
}

// addCACertToTransport は既存の HTTPTransport の TLS 設定に CA 証明書プールを追加する。
func addCACertToTransport(t *HTTPTransport, rootCAs *x509.CertPool) {
	if transport, ok := t.httpClient.Transport.(*http.Transport); ok {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.RootCAs = rootCAs
	}
}

// newNTLMTransport は NTLM 認証付きの HTTPTransport を作成する。
func newNTLMTransport(endpoint, username, password string) *HTTPTransport {
	baseTransport := optimizedTransport(&tls.Config{
		InsecureSkipVerify: true, //#nosec G402 -- WinRM は自己署名証明書が一般的
	})

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

// Put は WS-Transfer Put 操作を実行する。
// resourceURI で CIM クラスを指定し、selectors でインスタンスを特定し、
// properties で更新するプロパティを指定する。
func (c *Client) Put(ctx context.Context, resourceURI string, properties map[string]string, selectors ...Selector) (*GetResponse, error) {
	reqData, err := BuildPutRequest(resourceURI, c.endpoint, properties, selectors...)
	if err != nil {
		return nil, fmt.Errorf("failed to build Put request: %w", err)
	}

	respData, err := c.transport.Send(ctx, reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to send Put request: %w", err)
	}

	return ParsePutResponse(respData)
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
// opts で WQL フィルタ等を指定できる。
func (c *Client) Enumerate(ctx context.Context, resourceURI string, opts ...EnumerateOption) ([]*Instance, error) {
	// Step 1: Enumerate リクエスト
	enumReqData, err := BuildEnumerateRequest(resourceURI, c.endpoint, opts...)
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

// Create は WS-Transfer Create 操作を実行する。
// resourceURI で CIM クラスを指定し、properties で作成するインスタンスのプロパティを指定する。
func (c *Client) Create(ctx context.Context, resourceURI string, properties map[string]string) (*CreateResponse, error) {
	reqData, err := BuildCreateRequest(resourceURI, c.endpoint, properties)
	if err != nil {
		return nil, fmt.Errorf("failed to build Create request: %w", err)
	}

	respData, err := c.transport.Send(ctx, reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to send Create request: %w", err)
	}

	return ParseCreateResponse(respData)
}

// Invoke は CIM メソッド呼び出しを実行する。
// resourceURI で CIM クラスの URI を指定し、methodName で呼び出すメソッド名を指定する。
// params は入力パラメータ（nil または空の場合はパラメータなし）。
// selectors はインスタンスメソッドの場合のインスタンス特定用 SelectorSet。
//
// 同名要素を複数値で送る配列パラメータが必要な場合は InvokeMulti を使う。
func (c *Client) Invoke(ctx context.Context, resourceURI, methodName string, params map[string]string, selectors ...Selector) (*InvokeResponse, error) {
	reqData, err := BuildInvokeRequest(resourceURI, c.endpoint, methodName, params, selectors...)
	if err != nil {
		return nil, fmt.Errorf("failed to build Invoke request: %w", err)
	}

	respData, err := c.transport.Send(ctx, reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to send Invoke request: %w", err)
	}

	return ParseInvokeResponse(respData)
}

// InvokeMulti は []Param を受け取る CIM メソッド呼び出しを実行する。
//
// 同名要素を複数値で送る配列パラメータ (例: AddResourceSettings の ResourceSettings[])
// が必要な場合に使う。params の順序がそのまま XML の出現順になる。
func (c *Client) InvokeMulti(ctx context.Context, resourceURI, methodName string, params []Param, selectors ...Selector) (*InvokeResponse, error) {
	reqData, err := BuildInvokeRequestMulti(resourceURI, c.endpoint, methodName, params, selectors...)
	if err != nil {
		return nil, fmt.Errorf("failed to build Invoke request: %w", err)
	}

	respData, err := c.transport.Send(ctx, reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to send Invoke request: %w", err)
	}

	return ParseInvokeResponse(respData)
}

// Delete は WS-Transfer Delete 操作を実行する。
// resourceURI で CIM クラスを指定し、selectors でインスタンスを特定する。
func (c *Client) Delete(ctx context.Context, resourceURI string, selectors ...Selector) error {
	reqData, err := BuildDeleteRequest(resourceURI, c.endpoint, selectors...)
	if err != nil {
		return fmt.Errorf("failed to build Delete request: %w", err)
	}

	respData, err := c.transport.Send(ctx, reqData)
	if err != nil {
		return fmt.Errorf("failed to send Delete request: %w", err)
	}

	return ParseDeleteResponse(respData)
}
