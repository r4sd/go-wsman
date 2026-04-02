package wsman

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// CreateResponse は WS-Transfer CreateResponse を表す。
// 作成されたインスタンスの EndpointReference（ResourceURI + SelectorSet）を保持する。
type CreateResponse struct {
	ResourceURI string            // 作成されたインスタンスの ResourceURI
	Selectors   map[string]string // 作成されたインスタンスを特定する SelectorSet
}

// BuildCreateRequest は WS-Transfer Create リクエストの SOAP XML を生成する。
// Body に作成するインスタンスのプロパティを含む。
// SelectorSet は不要（新規作成なので特定するインスタンスがない）。
func BuildCreateRequest(resourceURI, endpoint string, properties map[string]string) ([]byte, error) {
	if len(properties) == 0 {
		return nil, fmt.Errorf("properties must not be empty")
	}

	opts := []Option{
		WithAction(ActionCreate),
		WithResourceURI(resourceURI),
		WithTo(endpoint),
		WithReplyTo(AddressAnonymous),
		WithMessageID("uuid:" + uuid.New().String()),
		WithMaxEnvelopeSize(153600),
		WithOperationTimeout("PT60S"),
	}

	env := NewEnvelope(opts...)

	// resourceURI からクラス名を抽出（最後の "/" 以降）
	className := resourceURI
	if idx := strings.LastIndex(resourceURI, "/"); idx >= 0 {
		className = resourceURI[idx+1:]
	}

	// プロパティ XML を構築
	// キーをソートしてテストの安定性を確保
	keys := make([]string, 0, len(properties))
	for k := range properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<p:%s xmlns:p="%s">`, className, resourceURI))
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf(`<p:%s>%s</p:%s>`, k, properties[k], k))
	}
	sb.WriteString(fmt.Sprintf(`</p:%s>`, className))

	env.SetBody([]byte("\n    " + sb.String() + "\n  "))

	return MarshalEnvelope(env)
}

// ParseCreateResponse は WS-Transfer CreateResponse の SOAP XML をパースする。
// 作成されたインスタンスの EndpointReference（ResourceURI + SelectorSet）を返す。
func ParseCreateResponse(data []byte) (*CreateResponse, error) {
	if IsFault(data) {
		fault, err := ParseFault(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse fault: %w", err)
		}
		return nil, fault
	}

	env, err := UnmarshalEnvelope(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Create response: %w", err)
	}

	resp := &CreateResponse{
		Selectors: make(map[string]string),
	}

	// Body の innerxml から EndpointReference を XML トークンベースで抽出
	if err := extractEndpointReference(env.Body.Content, resp); err != nil {
		return nil, fmt.Errorf("failed to extract EndpointReference: %w", err)
	}

	return resp, nil
}

// extractEndpointReference は CreateResponse の Body から ResourceURI と SelectorSet を抽出する。
// XML トークンベースのパースで、名前空間プレフィックスに依存しない。
func extractEndpointReference(data []byte, resp *CreateResponse) error {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))

	var inResourceURI bool
	var inSelector bool
	var currentSelectorName string

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to parse XML token: %w", err)
		}

		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "ResourceURI":
				inResourceURI = true
			case "Selector":
				inSelector = true
				for _, attr := range t.Attr {
					if attr.Name.Local == "Name" {
						currentSelectorName = attr.Value
					}
				}
			}
		case xml.CharData:
			value := strings.TrimSpace(string(t))
			if value == "" {
				continue
			}
			if inResourceURI {
				resp.ResourceURI = value
			}
			if inSelector && currentSelectorName != "" {
				resp.Selectors[currentSelectorName] = value
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "ResourceURI":
				inResourceURI = false
			case "Selector":
				inSelector = false
				currentSelectorName = ""
			}
		}
	}

	return nil
}
