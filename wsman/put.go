package wsman

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// BuildPutRequest は WS-Transfer Put リクエストの SOAP XML を生成する。
// resourceURI: CIM クラスの URI
// endpoint: WinRM エンドポイント URL
// properties: 更新するプロパティ（map[string]string）
// selectors: インスタンスを特定する SelectorSet
func BuildPutRequest(resourceURI, endpoint string, properties map[string]string, selectors ...Selector) ([]byte, error) {
	if len(properties) == 0 {
		return nil, fmt.Errorf("properties must not be empty")
	}

	opts := []Option{
		WithAction(ActionPut),
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
	fmt.Fprintf(&sb, `<p:%s xmlns:p="%s">`, className, resourceURI)
	for _, k := range keys {
		fmt.Fprintf(&sb, `<p:%s>%s</p:%s>`, k, properties[k], k)
	}
	fmt.Fprintf(&sb, `</p:%s>`, className)

	env.SetBody([]byte("\n    " + sb.String() + "\n  "))

	return MarshalEnvelope(env)
}

// ParsePutResponse は WS-Transfer PutResponse の SOAP XML をパースする。
// GetResponse と同じ形式（更新後の全プロパティを含む）。
func ParsePutResponse(data []byte) (*GetResponse, error) {
	return ParseGetResponse(data)
}
