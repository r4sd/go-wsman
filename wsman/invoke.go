package wsman

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// InvokeResponse は CIM Invoke の結果を表す。
type InvokeResponse struct {
	// ReturnValue はメソッドの戻り値。"0" は成功、"4096" は非同期ジョブを示す。
	ReturnValue string
	properties  map[string]string
}

// Property は指定された出力パラメータの値を返す。存在しない場合は空文字列を返す。
func (r *InvokeResponse) Property(name string) string {
	return r.properties[name]
}

// Properties は全出力パラメータを map として返す。
func (r *InvokeResponse) Properties() map[string]string {
	result := make(map[string]string, len(r.properties))
	for k, v := range r.properties {
		result[k] = v
	}
	return result
}

// BuildInvokeRequest は CIM Invoke リクエストの SOAP XML を生成する。
// resourceURI: CIM クラスの URI
// endpoint: WinRM エンドポイント URL
// methodName: 呼び出すメソッド名（例: "DefineSystem"）
// params: 入力パラメータ（map[string]string）。nil または空の場合はパラメータなし。
// selectors: インスタンスメソッドの場合のインスタンス特定用 SelectorSet
func BuildInvokeRequest(resourceURI, endpoint, methodName string, params map[string]string, selectors ...Selector) ([]byte, error) {
	if methodName == "" {
		return nil, fmt.Errorf("methodName must not be empty")
	}

	// Action URI: resourceURI/methodName
	actionURI := resourceURI + "/" + methodName

	opts := []Option{
		WithAction(actionURI),
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

	// Body: MethodName_INPUT 要素を構築
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<p:%s_INPUT xmlns:p="%s">`, methodName, resourceURI))

	if len(params) > 0 {
		// パラメータキーをソートしてテストの安定性を確保
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			sb.WriteString(fmt.Sprintf(`<p:%s>%s</p:%s>`, k, params[k], k))
		}
	}

	sb.WriteString(fmt.Sprintf(`</p:%s_INPUT>`, methodName))

	env.SetBody([]byte("\n    " + sb.String() + "\n  "))

	return MarshalEnvelope(env)
}

// ParseInvokeResponse は CIM Invoke レスポンスの SOAP XML をパースする。
// Body 内の _OUTPUT 要素から ReturnValue と出力パラメータを抽出する。
func ParseInvokeResponse(data []byte) (*InvokeResponse, error) {
	if IsFault(data) {
		fault, err := ParseFault(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse fault: %w", err)
		}
		return nil, fault
	}

	env, err := UnmarshalEnvelope(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Invoke response: %w", err)
	}

	resp := &InvokeResponse{
		properties: make(map[string]string),
	}

	// extractProperties は depth==2 の子要素のテキストを抽出する。
	// _OUTPUT ルート要素名は動的（メソッド名依存）だが、
	// extractProperties はルート要素名に関係なく子要素を抽出するので問題ない。
	if err := extractProperties(env.Body.Content, resp.properties); err != nil {
		return nil, fmt.Errorf("failed to extract Invoke output properties: %w", err)
	}

	// ReturnValue を特別扱い: properties から取り出して専用フィールドに格納
	if rv, ok := resp.properties["ReturnValue"]; ok {
		resp.ReturnValue = rv
		delete(resp.properties, "ReturnValue")
	}

	return resp, nil
}
