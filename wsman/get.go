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
	properties map[string][]string
}

// Property は指定されたプロパティの値を返す。存在しない場合は空文字列を返す。
// 配列プロパティの場合は最後の値を返す (後方互換)。配列を扱うには PropertiesList を使うこと。
func (r *GetResponse) Property(name string) string {
	vs := r.properties[name]
	if len(vs) == 0 {
		return ""
	}
	return vs[len(vs)-1]
}

// Properties は全プロパティを map として返す。
// 配列プロパティは最後の値のみが含まれる (後方互換)。配列を扱うには PropertiesList を使うこと。
func (r *GetResponse) Properties() map[string]string {
	result := make(map[string]string, len(r.properties))
	for k, vs := range r.properties {
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
func (r *GetResponse) PropertiesList() map[string][]string {
	result := make(map[string][]string, len(r.properties))
	for k, vs := range r.properties {
		dup := make([]string, len(vs))
		copy(dup, vs)
		result[k] = dup
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
		properties: make(map[string][]string),
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
//
// 同名要素 (CIM 配列プロパティ) は順序を保ったまま slice に追加する。
// プロパティが入れ子 XML を含む場合 (EPR 等)、入れ子内の最後の非空テキストを値とする
// (後方互換: 旧実装が Job プロパティから InstanceID を取り出す慣習に依存しているため)。
func extractProperties(data []byte, props map[string][]string) error {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))

	var currentElement string
	var lastNonEmpty string
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
				lastNonEmpty = ""
			}
		case xml.CharData:
			if currentElement != "" {
				if v := strings.TrimSpace(string(t)); v != "" {
					lastNonEmpty = v
				}
			}
		case xml.EndElement:
			if depth == 2 && currentElement != "" {
				if lastNonEmpty != "" {
					props[currentElement] = append(props[currentElement], lastNonEmpty)
				}
				currentElement = ""
				lastNonEmpty = ""
			}
			depth--
		}
	}

	return nil
}
