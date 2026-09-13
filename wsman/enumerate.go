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

// wqlWildcardResourceURI は WQL フィルタ列挙用に ResourceURI のクラス名を '*' に置換する。
//
// Microsoft WS-Man は WQL Dialect のフィルタを使う列挙で、ResourceURI のクラス名を '*'
// (ワイルドカード) にすることを要求する (実クラスは WQL の FROM 句で指定)。具体クラス URI
// + WQL フィルタは「リソース URI にはキーを含めることはできず、クラス名は '*' でなければ
// なりません」の Fault になる (実機 Hyper-V で確認)。namespace 部分は保持し、末尾の
// クラスセグメントのみ '*' に置換する。
func wqlWildcardResourceURI(resourceURI string) string {
	if i := strings.LastIndex(resourceURI, "/"); i >= 0 {
		return resourceURI[:i+1] + "*"
	}
	return resourceURI
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

	// WQL フィルタ使用時は ResourceURI のクラス名を '*' に置換する (実クラスは WQL の
	// FROM 句で指定)。具体クラス URI + WQL は MS WS-Man で Fault になるため。
	uri := resourceURI
	if cfg.wqlFilterSet {
		uri = wqlWildcardResourceURI(resourceURI)
	}

	env := NewEnvelope(
		WithAction(ActionEnumerate),
		WithResourceURI(uri),
		WithTo(endpoint),
		WithReplyTo(AddressAnonymous),
		WithMessageID("uuid:"+uuid.New().String()),
		WithMaxEnvelopeSize(153600),
		WithOperationTimeout("PT60S"),
	)

	// Enumerate ボディ要素
	var body string
	if cfg.wqlFilter != "" {
		// WQL フィルタは XML テキストとして埋め込むため、& や < を含む表示名でも
		// SOAP が壊れないよう XML エスケープする。WQL リテラル内の引用符の
		// エスケープ (" → \") は呼び出し側 (hyperv 層) の責務。
		body = fmt.Sprintf(
			`<n:Enumerate xmlns:n="%s"><w:Filter Dialect="%s">%s</w:Filter></n:Enumerate>`,
			NSEnumeration,
			DialectWQL,
			escapeXMLText(cfg.wqlFilter),
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

// xmlSchemaInstanceNS は xsi:nil 属性の namespace URI。
const xmlSchemaInstanceNS = "http://www.w3.org/2001/XMLSchema-instance"

// nullPlaceholder は xsi:nil="true" のプロパティの位置だけを確保しておく内部センチネル。
// NUL 文字を含むため CIM の実値と衝突しない。インスタンス確定時に
// resolveNullPlaceholders が解決するので、Instance の外には漏れない。
const nullPlaceholder = "\x00wsman:null\x00"

// parseInstances は Items の innerxml から個別の CIM インスタンスを抽出する。
// 同名要素 (CIM 配列プロパティ) は順序を保ったまま slice に追加する。
// プロパティが入れ子 XML を含む場合は入れ子内の最後の非空テキストを値とする (extractProperties と同じ後方互換挙動)。
//
// xsi:nil="true" の扱いは resolveNullPlaceholders を参照 (#141)。
func parseInstances(data []byte) ([]*Instance, error) { //nolint:gocognit // XML トークンストリームの状態機械 (depth 追跡 + token 種別 switch)。分割は可読性を損なう
	decoder := xml.NewDecoder(bytes.NewReader(data))

	var instances []*Instance
	var currentInstance *Instance
	var currentProp string
	var lastNonEmpty string
	var currentNil bool
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
				currentNil = isXSINil(t.Attr)
			}
		case xml.CharData:
			if currentProp != "" && currentInstance != nil {
				if v := strings.TrimSpace(string(t)); v != "" {
					lastNonEmpty = v
				}
			}
		case xml.EndElement:
			if depth == 2 && currentInstance != nil && currentProp != "" {
				switch {
				case lastNonEmpty != "":
					currentInstance.properties[currentProp] = append(currentInstance.properties[currentProp], lastNonEmpty)
				case currentNil:
					// 位置だけ確保する。配列の途中が NULL でも後続の index がずれないようにするため。
					currentInstance.properties[currentProp] = append(currentInstance.properties[currentProp], nullPlaceholder)
				}
				currentProp = ""
				lastNonEmpty = ""
				currentNil = false
			} else if depth == 1 && currentInstance != nil {
				resolveNullPlaceholders(currentInstance.properties)
				instances = append(instances, currentInstance)
				currentInstance = nil
			}
			depth--
		}
	}

	return instances, nil
}

// escapeXMLText は文字列を XML テキスト内容として安全な形にエスケープする。
//
// & < > " ' を実体参照に変換する。WQL フィルタを SOAP ボディへ埋め込む際に使用し、
// 特殊文字を含む表示名でも整形式 XML を保つ。
func escapeXMLText(s string) string {
	var sb strings.Builder
	if err := xml.EscapeText(&sb, []byte(s)); err != nil {
		return s
	}
	return sb.String()
}

// isXSINil は要素が xsi:nil="true" を持つか判定する。
// namespace URI で判定するので prefix の付け方 (xsi / i など) には依存しない。
func isXSINil(attrs []xml.Attr) bool {
	for _, a := range attrs {
		// Items の innerxml を単独でパースする都合上、祖先 (Envelope) 側でしか
		// xmlns:xsi が宣言されていない応答では prefix が解決されず、Space に
		// prefix 文字列がそのまま残る。両方受ける。
		if a.Name.Local == "nil" && (a.Name.Space == xmlSchemaInstanceNS || a.Name.Space == "xsi") {
			return strings.EqualFold(a.Value, "true") || a.Value == "1"
		}
	}
	return false
}

// resolveNullPlaceholders は nullPlaceholder を確定した値に置き換える (#141)。
//
//   - 全要素が NULL のプロパティ → **キーごと削除**。実機は NULL スカラーを
//     <p:Parent xsi:nil="true"/> の形で返すため (実機応答 570 件中 531 件が該当)、
//     空文字 1 要素にすると uint 系フィールドで ParseUint("") エラー、ポインタ
//     フィールドでは「明示的にゼロ値を送る」という誤った意味になる。
//   - NULL と非 NULL が混在するプロパティ → NULL を空文字にして **位置を保つ**。
//     並列配列 (IPAddresses / Subnets / DNSServers 等) で index がずれると、
//     別エントリの値として読まれてしまうため。
func resolveNullPlaceholders(props map[string][]string) {
	for name, values := range props {
		allNull := true
		for _, v := range values {
			if v != nullPlaceholder {
				allNull = false
				break
			}
		}
		if allNull {
			delete(props, name)
			continue
		}
		for i, v := range values {
			if v == nullPlaceholder {
				values[i] = ""
			}
		}
	}
}
