package wsman

import (
	"fmt"

	"github.com/google/uuid"
)

// BuildDeleteRequest は WS-Transfer Delete リクエストの SOAP XML を生成する。
// Body は空。SelectorSet でインスタンスを特定する。
func BuildDeleteRequest(resourceURI, endpoint string, selectors ...Selector) ([]byte, error) {
	opts := []Option{
		WithAction(ActionDelete),
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

// ParseDeleteResponse は WS-Transfer DeleteResponse の SOAP XML をパースする。
// 成功の場合はエラーなし、Fault の場合はエラーを返す。
func ParseDeleteResponse(data []byte) error {
	if IsFault(data) {
		fault, err := ParseFault(data)
		if err != nil {
			return fmt.Errorf("failed to parse fault: %w", err)
		}
		return fault
	}

	return nil
}
