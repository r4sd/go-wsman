package wsman

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Instance は CIM インスタンスを表す（プロパティの map）
type Instance struct {
	properties map[string]string
}

// Property は指定されたプロパティの値を返す。存在しない場合は空文字列を返す。
func (i *Instance) Property(name string) string {
	return i.properties[name]
}

// Properties は全プロパティを map として返す
func (i *Instance) Properties() map[string]string {
	result := make(map[string]string, len(i.properties))
	for k, v := range i.properties {
		result[k] = v
	}
	return result
}

// PullResponse は WS-Enumeration Pull レスポンスを表す
type PullResponse struct {
	Items              []*Instance
	EnumerationContext  string
	EndOfSequence      bool
}

// BuildEnumerateRequest は WS-Enumeration Enumerate リクエストの SOAP XML を生成する
func BuildEnumerateRequest(resourceURI, endpoint string) ([]byte, error) {
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
	body := fmt.Sprintf(
		`<n:Enumerate xmlns:n="%s"></n:Enumerate>`,
		NSEnumeration,
	)
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
			return "", fmt.Errorf("Fault のパースに失敗: %w", err)
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
		return "", fmt.Errorf("EnumerateResponse のパースに失敗: %w", err)
	}

	ctx := resp.Body.EnumerateResponse.EnumerationContext
	if ctx == "" {
		return "", fmt.Errorf("EnumerationContext が見つかりません")
	}

	return ctx, nil
}

// ParsePullResponse は PullResponse をパースする
func ParsePullResponse(data []byte) (*PullResponse, error) {
	if IsFault(data) {
		fault, err := ParseFault(data)
		if err != nil {
			return nil, fmt.Errorf("Fault のパースに失敗: %w", err)
		}
		return nil, fault
	}

	// PullResponse の基本構造をパース
	type pullResp struct {
		XMLName xml.Name `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
		Body    struct {
			PullResponse struct {
				Items              struct {
					Content []byte `xml:",innerxml"`
				} `xml:"http://schemas.xmlsoap.org/ws/2004/09/enumeration Items"`
				EnumerationContext string `xml:"http://schemas.xmlsoap.org/ws/2004/09/enumeration EnumerationContext"`
				EndOfSequence      *struct{} `xml:"http://schemas.xmlsoap.org/ws/2004/09/enumeration EndOfSequence"`
			} `xml:"http://schemas.xmlsoap.org/ws/2004/09/enumeration PullResponse"`
		} `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
	}

	var pr pullResp
	if err := xml.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("PullResponse のパースに失敗: %w", err)
	}

	result := &PullResponse{
		EnumerationContext: pr.Body.PullResponse.EnumerationContext,
		EndOfSequence:      pr.Body.PullResponse.EndOfSequence != nil,
	}

	// Items 内の CIM インスタンスを抽出
	if len(pr.Body.PullResponse.Items.Content) > 0 {
		instances, err := parseInstances(pr.Body.PullResponse.Items.Content)
		if err != nil {
			return nil, fmt.Errorf("CIM インスタンスの抽出に失敗: %w", err)
		}
		result.Items = instances
	}

	return result, nil
}

// parseInstances は Items の innerxml から個別の CIM インスタンスを抽出する
func parseInstances(data []byte) ([]*Instance, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))

	var instances []*Instance
	var currentInstance *Instance
	var currentProp string
	depth := 0

	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				// CIM インスタンスの開始
				currentInstance = &Instance{
					properties: make(map[string]string),
				}
			} else if depth == 2 && currentInstance != nil {
				// プロパティ要素の開始
				currentProp = t.Name.Local
			}
		case xml.CharData:
			if currentProp != "" && currentInstance != nil {
				value := strings.TrimSpace(string(t))
				if value != "" {
					currentInstance.properties[currentProp] = value
				}
			}
		case xml.EndElement:
			if depth == 2 {
				currentProp = ""
			} else if depth == 1 && currentInstance != nil {
				instances = append(instances, currentInstance)
				currentInstance = nil
			}
			depth--
		}
	}

	return instances, nil
}
