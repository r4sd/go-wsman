package wsman

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

// Fault は SOAP Fault を表し、error インターフェースを実装する
type Fault struct {
	Code    string // 例: "s:Sender"
	Subcode string // 例: "w:AccessDenied"
	Reason  string // 例: "Access is denied."
	Detail  string // WSManFault の詳細（あれば）
}

// Error は error インターフェースの実装
func (f *Fault) Error() string {
	if f.Subcode != "" {
		return fmt.Sprintf("WS-Man Fault [%s/%s]: %s", f.Code, f.Subcode, f.Reason)
	}
	return fmt.Sprintf("WS-Man Fault [%s]: %s", f.Code, f.Reason)
}

// soapFaultEnvelope は SOAP Fault レスポンスのパース用構造体
type soapFaultEnvelope struct {
	XMLName xml.Name      `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Body    soapFaultBody `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
}

type soapFaultBody struct {
	Fault *soapFault `xml:"http://www.w3.org/2003/05/soap-envelope Fault"`
}

type soapFault struct {
	Code   soapFaultCode   `xml:"http://www.w3.org/2003/05/soap-envelope Code"`
	Reason soapFaultReason `xml:"http://www.w3.org/2003/05/soap-envelope Reason"`
	Detail soapFaultDetail `xml:"http://www.w3.org/2003/05/soap-envelope Detail"`
}

type soapFaultCode struct {
	Value   string            `xml:"http://www.w3.org/2003/05/soap-envelope Value"`
	Subcode *soapFaultSubcode `xml:"http://www.w3.org/2003/05/soap-envelope Subcode"`
}

type soapFaultSubcode struct {
	Value string `xml:"http://www.w3.org/2003/05/soap-envelope Value"`
}

type soapFaultReason struct {
	Text string `xml:"http://www.w3.org/2003/05/soap-envelope Text"`
}

type soapFaultDetail struct {
	Content []byte `xml:",innerxml"`
}

// ParseFault は SOAP XML バイト列から Fault をパースする。
// Fault が含まれていない場合はエラーを返す。
func ParseFault(data []byte) (*Fault, error) {
	var env soapFaultEnvelope
	if err := xml.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("SOAP XML のパースに失敗: %w", err)
	}

	if env.Body.Fault == nil {
		return nil, fmt.Errorf("SOAP レスポンスに Fault が含まれていません")
	}

	f := &Fault{
		Code:   env.Body.Fault.Code.Value,
		Reason: env.Body.Fault.Reason.Text,
		Detail: string(env.Body.Fault.Detail.Content),
	}

	if env.Body.Fault.Code.Subcode != nil {
		f.Subcode = env.Body.Fault.Code.Subcode.Value
	}

	return f, nil
}

// IsFault は SOAP XML バイト列が Fault を含むかどうかを簡易判定する。
// 完全なパースは行わず、XML 内に Fault 要素が存在するかをバイト検索で判定する。
func IsFault(data []byte) bool {
	return bytes.Contains(data, []byte(":Fault"))
}
