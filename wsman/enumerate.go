package wsman

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
)

// Instance は CIM インスタンスを表す（プロパティの map）
type Instance struct {
	properties map[string][]string
}

// Property は指定されたプロパティの値を返す。存在しない場合は空文字列を返す。
// 配列プロパティの場合は最後の値を返す (後方互換)。配列を扱うには PropertiesList を使うこと。
func (i *Instance) Property(name string) string {
	vs := i.properties[name]
	if len(vs) == 0 {
		return ""
	}
	return vs[len(vs)-1]
}

// Properties は全プロパティを map として返す。
// 配列プロパティは最後の値のみが含まれる (後方互換)。配列を扱うには PropertiesList を使うこと。
func (i *Instance) Properties() map[string]string {
	result := make(map[string]string, len(i.properties))
	for k, vs := range i.properties {
		if len(vs) == 0 {
			continue
		}
		result[k] = vs[len(vs)-1]
	}
	return result
}

// PropertiesList は全プロパティを map[string][]string として返す。
// 同名要素の繰り返し (CIM の string[] / uint16[] 等) を保持する。
// hyperv.UnmarshalList と組み合わせて配列フィールド対応の構造体にマッピングできる。
func (i *Instance) PropertiesList() map[string][]string {
	result := make(map[string][]string, len(i.properties))
	for k, vs := range i.properties {
		dup := make([]string, len(vs))
		copy(dup, vs)
		result[k] = dup
	}
	return result
}

// PullResponse は WS-Enumeration Pull レスポンスを表す
type PullResponse struct {
	Items              []*Instance
	EnumerationContext string
	EndOfSequence      bool
}

// EnumerateOption は Enumerate リクエストのオプション
type EnumerateOption func(*enumerateConfig)

// enumerateConfig は Enumerate リクエストの設定
type enumerateConfig struct {
	wqlFilter    string
	wqlFilterSet bool // WithWQL が明示的に呼ばれたかどうか
}

// WithWQL は WQL (WMI Query Language) フィルタを設定する。
// 例: WithWQL("SELECT * FROM Win32_Service WHERE State = 'Running'")
func WithWQL(query string) EnumerateOption {
	return func(cfg *enumerateConfig) {
		cfg.wqlFilter = query
		cfg.wqlFilterSet = true
	}
}

// BuildEnumerateRequest は WS-Enumeration Enumerate リクエストの SOAP XML を生成する。
// opts で WQL フィルタ等のオプションを指定できる。
func BuildEnumerateRequest(resourceURI, endpoint string, opts ...EnumerateOption) ([]byte, error) {
	var cfg enumerateConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.wqlFilterSet && cfg.wqlFilter == "" {
		return nil, fmt.Errorf("WQL query must not be empty")
	}

	env := NewEnvelope(
		WithAction(ActionEnumerate),
		WithResourceURI(resourceURI),
		WithTo(endpoint),
		WithReplyTo(AddressAnonymous),
		WithMessageID("uuid:"+uuid.New().String()),
		WithMaxEnvelopeSize(153600),
		WithOperationTimeout("PT60S"),
	)

	// Enumerate ボディ要素
	var body string
	if cfg.wqlFilter != "" {
		body = fmt.Sprintf(
			`<n:Enumerate xmlns:n="%s"><w:Filter Dialect="%s">%s</w:Filter></n:Enumerate>`,
			NSEnumeration,
			DialectWQL,
			cfg.wqlFilter,
		)
	} else {
		body = fmt.Sprintf(
			`<n:Enumerate xmlns:n="%s"></n:Enumerate>`,
			NSEnumeration,
		)
	}
	env.SetBody([]byte("\n    " + body + "\n  "))

	return MarshalEnvelope(env)
}

// BuildPullRequest は WS-Enumeration Pull リクエストの SOAP XML を生成する
func BuildPullRequest(resourceURI, endpoint, enumerationContext string) ([]byte, error) {
	env := NewEnvelope(
		WithAction(ActionPull),
		WithResourceURI(resourceURI),
		WithTo(endpoint),
		WithReplyTo(AddressAnonymous),
		WithMessageID("uuid:"+uuid.New().String()),
		WithMaxEnvelopeSize(153600),
		WithOperationTimeout("PT60S"),
	)

	// Pull ボディ要素
	body := fmt.Sprintf(
		`<n:Pull xmlns:n="%s"><n:EnumerationContext>%s</n:EnumerationContext></n:Pull>`,
		NSEnumeration,
		enumerationContext,
	)
	env.SetBody([]byte("\n    " + body + "\n  "))

	return MarshalEnvelope(env)
}

// ParseEnumerateResponse は EnumerateResponse から EnumerationContext を抽出する
func ParseEnumerateResponse(data []byte) (string, error) {
	if IsFault(data) {
		fault, err := ParseFault(data)
		if err != nil {
			return "", fmt.Errorf("failed to parse fault: %w", err)
		}
		return "", fault
	}

	// EnumerationContext をパース
	type enumResponse struct {
		XMLName xml.Name `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
		Body    struct {
			EnumerateResponse struct {
				EnumerationContext string `xml:"http://schemas.xmlsoap.org/ws/2004/09/enumeration EnumerationContext"`
			} `xml:"http://schemas.xmlsoap.org/ws/2004/09/enumeration EnumerateResponse"`
		} `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
	}

	var resp enumResponse
	if err := xml.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("failed to parse EnumerateResponse: %w", err)
	}

	ctx := resp.Body.EnumerateResponse.EnumerationContext
	if ctx == "" {
		return "", fmt.Errorf("EnumerationContext not found in response")
	}

	return ctx, nil
}

// ParsePullResponse は PullResponse をパースする
func ParsePullResponse(data []byte) (*PullResponse, error) {
	if IsFault(data) {
		fault, err := ParseFault(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse fault: %w", err)
		}
		return nil, fault
	}

	// PullResponse の基本構造をパース
	type pullResp struct {
		XMLName xml.Name `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
		Body    struct {
			PullResponse struct {
				Items struct {
					Content []byte `xml:",innerxml"`
				} `xml:"http://schemas.xmlsoap.org/ws/2004/09/enumeration Items"`
				EnumerationContext string    `xml:"http://schemas.xmlsoap.org/ws/2004/09/enumeration EnumerationContext"`
				EndOfSequence      *struct{} `xml:"http://schemas.xmlsoap.org/ws/2004/09/enumeration EndOfSequence"`
			} `xml:"http://schemas.xmlsoap.org/ws/2004/09/enumeration PullResponse"`
		} `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
	}

	var pr pullResp
	if err := xml.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse PullResponse: %w", err)
	}

	result := &PullResponse{
		EnumerationContext: pr.Body.PullResponse.EnumerationContext,
		EndOfSequence:      pr.Body.PullResponse.EndOfSequence != nil,
	}

	// Items 内の CIM インスタンスを抽出
	if len(pr.Body.PullResponse.Items.Content) > 0 {
		instances, err := parseInstances(pr.Body.PullResponse.Items.Content)
		if err != nil {
			return nil, fmt.Errorf("failed to extract CIM instances: %w", err)
		}
		result.Items = instances
	}

	return result, nil
}

// parseInstances は Items の innerxml から個別の CIM インスタンスを抽出する。
// 同名要素 (CIM 配列プロパティ) は順序を保ったまま slice に追加する。
// プロパティが入れ子 XML を含む場合は入れ子内の最後の非空テキストを値とする (extractProperties と同じ後方互換挙動)。
func parseInstances(data []byte) ([]*Instance, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))

	var instances []*Instance
	var currentInstance *Instance
	var currentProp string
	var lastNonEmpty string
	depth := 0

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to parse XML token: %w", err)
		}

		switch t := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				// CIM インスタンスの開始
				currentInstance = &Instance{
					properties: make(map[string][]string),
				}
			} else if depth == 2 && currentInstance != nil {
				// プロパティ要素の開始
				currentProp = t.Name.Local
				lastNonEmpty = ""
			}
		case xml.CharData:
			if currentProp != "" && currentInstance != nil {
				if v := strings.TrimSpace(string(t)); v != "" {
					lastNonEmpty = v
				}
			}
		case xml.EndElement:
			if depth == 2 && currentInstance != nil && currentProp != "" {
				if lastNonEmpty != "" {
					currentInstance.properties[currentProp] = append(currentInstance.properties[currentProp], lastNonEmpty)
				}
				currentProp = ""
				lastNonEmpty = ""
			} else if depth == 1 && currentInstance != nil {
				instances = append(instances, currentInstance)
				currentInstance = nil
			}
			depth--
		}
	}

	return instances, nil
}
