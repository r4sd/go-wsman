package wsman

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
)

// GetResponse は WS-Transfer Get レスポンスを表す
type GetResponse struct {
	properties map[string]string
}

// Property は指定されたプロパティの値を返す。存在しない場合は空文字列を返す。
func (r *GetResponse) Property(name string) string {
	return r.properties[name]
}

// Properties は全プロパティを map として返す
func (r *GetResponse) Properties() map[string]string {
	result := make(map[string]string, len(r.properties))
	for k, v := range r.properties {
		result[k] = v
	}
	return result
}

// BuildGetRequest は WS-Transfer Get リクエストの SOAP XML を生成する
func BuildGetRequest(resourceURI, endpoint string, selectors ...Selector) ([]byte, error) {
	opts := []Option{
		WithAction(ActionGet),
		WithResourceURI(resourceURI),
		WithTo(endpoint),
		WithReplyTo(AddressAnonymous),
		WithMessageID("uuid:" + uuid.New().String()),
		WithMaxEnvelopeSize(153600),
		WithOperationTimeout("PT60S"),
	}

	for _, s := range selectors {
		opts = append(opts, WithSelector(s.Name, s.Value))
	}

	env := NewEnvelope(opts...)
	return MarshalEnvelope(env)
}

// ParseGetResponse は WS-Transfer GetResponse の SOAP XML をパースする。
// Fault を含む場合は *Fault をエラーとして返す。
func ParseGetResponse(data []byte) (*GetResponse, error) {
	if IsFault(data) {
		fault, err := ParseFault(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse fault: %w", err)
		}
		return nil, fault
	}

	env, err := UnmarshalEnvelope(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Get response: %w", err)
	}

	resp := &GetResponse{
		properties: make(map[string]string),
	}

	// Body の innerxml からプロパティを抽出
	if err := extractProperties(env.Body.Content, resp.properties); err != nil {
		return nil, fmt.Errorf("failed to extract properties: %w", err)
	}

	return resp, nil
}

// extractProperties は XML バイト列から CIM プロパティを抽出する。
// CIM レスポンスのプロパティは名前空間付きの要素として返されるため、
// ローカル名のみを使ってマップに格納する。
func extractProperties(data []byte, props map[string]string) error {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))

	var currentElement string
	depth := 0

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
			depth++
			if depth == 2 {
				// CIM インスタンスの直下のプロパティ要素
				currentElement = t.Name.Local
			}
		case xml.CharData:
			if currentElement != "" {
				value := strings.TrimSpace(string(t))
				if value != "" {
					props[currentElement] = value
				}
			}
		case xml.EndElement:
			if depth == 2 {
				currentElement = ""
			}
			depth--
		}
	}

	return nil
}
