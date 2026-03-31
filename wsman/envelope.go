package wsman

import (
	"encoding/xml"
	"fmt"
)

// Envelope は SOAP 1.2 エンベロープを表す。
// Marshal 時は名前空間プレフィックスを使った SOAP XML を生成し、
// Unmarshal 時は名前空間 URI ベースでパースする。
type Envelope struct {
	Header Header
	Body   Body
}

// marshalEnvelope は Marshal 用の内部構造体（プレフィックス付き XML タグ）
type marshalEnvelope struct {
	XMLName xml.Name      `xml:"s:Envelope"`
	NS      string        `xml:"xmlns:s,attr"`
	NSAddr  string        `xml:"xmlns:a,attr"`
	NSWsman string        `xml:"xmlns:w,attr"`
	Header  marshalHeader `xml:"s:Header"`
	Body    marshalBody   `xml:"s:Body"`
}

// unmarshalEnvelope は Unmarshal 用の内部構造体（名前空間 URI ベース）
type unmarshalEnvelope struct {
	XMLName xml.Name        `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Header  unmarshalHeader `xml:"http://www.w3.org/2003/05/soap-envelope Header"`
	Body    unmarshalBody   `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
}

// MarshalXML は Envelope をプレフィックス付き SOAP XML にシリアライズする
func (env *Envelope) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	m := marshalEnvelope{
		NS:      NSEnvelope,
		NSAddr:  NSAddressing,
		NSWsman: NSWsman,
		Header:  env.Header.toMarshal(),
		Body:    marshalBody{Content: env.Body.Content},
	}
	return e.Encode(m)
}

// UnmarshalXML は SOAP XML を Envelope にデシリアライズする
func (env *Envelope) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var u unmarshalEnvelope
	if err := d.DecodeElement(&u, &start); err != nil {
		return fmt.Errorf("SOAP Envelope のパースに失敗: %w", err)
	}
	env.Header = u.Header.toHeader()
	env.Body = Body{Content: u.Body.Content}
	return nil
}

// --- Header ---

// Header は SOAP ヘッダーを表す
type Header struct {
	Action           *Action
	ResourceURI      *ResourceURI
	MaxEnvelopeSize  *MaxEnvelopeSize
	OperationTimeout *OperationTimeout
	MessageID        *MessageID
	ReplyTo          *ReplyTo
	To               *To
	SelectorSet      *SelectorSet
}

type marshalHeader struct {
	Action           *marshalAction           `xml:"a:Action,omitempty"`
	ResourceURI      *marshalResourceURI      `xml:"w:ResourceURI,omitempty"`
	MaxEnvelopeSize  *marshalMaxEnvelopeSize  `xml:"w:MaxEnvelopeSize,omitempty"`
	OperationTimeout *marshalOperationTimeout `xml:"w:OperationTimeout,omitempty"`
	MessageID        *marshalMessageID        `xml:"a:MessageID,omitempty"`
	ReplyTo          *marshalReplyTo          `xml:"a:ReplyTo,omitempty"`
	To               *marshalTo               `xml:"a:To,omitempty"`
	SelectorSet      *marshalSelectorSet      `xml:"w:SelectorSet,omitempty"`
}

type unmarshalHeader struct {
	Action           *unmarshalAction           `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing Action"`
	ResourceURI      *unmarshalResourceURI      `xml:"http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd ResourceURI"`
	MaxEnvelopeSize  *unmarshalMaxEnvelopeSize  `xml:"http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd MaxEnvelopeSize"`
	OperationTimeout *unmarshalOperationTimeout `xml:"http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd OperationTimeout"`
	MessageID        *unmarshalMessageID        `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing MessageID"`
	ReplyTo          *unmarshalReplyTo          `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing ReplyTo"`
	To               *unmarshalTo               `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing To"`
	SelectorSet      *unmarshalSelectorSet      `xml:"http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd SelectorSet"`
}

func (h *Header) toMarshal() marshalHeader {
	m := marshalHeader{}
	if h.Action != nil {
		m.Action = &marshalAction{MustUnderstand: h.Action.MustUnderstand, Value: h.Action.Value}
	}
	if h.ResourceURI != nil {
		m.ResourceURI = &marshalResourceURI{MustUnderstand: h.ResourceURI.MustUnderstand, Value: h.ResourceURI.Value}
	}
	if h.MaxEnvelopeSize != nil {
		m.MaxEnvelopeSize = &marshalMaxEnvelopeSize{MustUnderstand: h.MaxEnvelopeSize.MustUnderstand, Value: h.MaxEnvelopeSize.Value}
	}
	if h.OperationTimeout != nil {
		m.OperationTimeout = &marshalOperationTimeout{Value: h.OperationTimeout.Value}
	}
	if h.MessageID != nil {
		m.MessageID = &marshalMessageID{Value: h.MessageID.Value}
	}
	if h.To != nil {
		m.To = &marshalTo{MustUnderstand: h.To.MustUnderstand, Value: h.To.Value}
	}
	if h.ReplyTo != nil {
		m.ReplyTo = &marshalReplyTo{
			Address: marshalAddress{MustUnderstand: h.ReplyTo.Address.MustUnderstand, Value: h.ReplyTo.Address.Value},
		}
	}
	if h.SelectorSet != nil {
		ms := &marshalSelectorSet{}
		for _, s := range h.SelectorSet.Selectors {
			ms.Selectors = append(ms.Selectors, marshalSelector(s))
		}
		m.SelectorSet = ms
	}
	return m
}

func (u *unmarshalHeader) toHeader() Header {
	h := Header{}
	if u.Action != nil {
		h.Action = &Action{MustUnderstand: u.Action.MustUnderstand, Value: u.Action.Value}
	}
	if u.ResourceURI != nil {
		h.ResourceURI = &ResourceURI{MustUnderstand: u.ResourceURI.MustUnderstand, Value: u.ResourceURI.Value}
	}
	if u.MaxEnvelopeSize != nil {
		h.MaxEnvelopeSize = &MaxEnvelopeSize{MustUnderstand: u.MaxEnvelopeSize.MustUnderstand, Value: u.MaxEnvelopeSize.Value}
	}
	if u.OperationTimeout != nil {
		h.OperationTimeout = &OperationTimeout{Value: u.OperationTimeout.Value}
	}
	if u.MessageID != nil {
		h.MessageID = &MessageID{Value: u.MessageID.Value}
	}
	if u.To != nil {
		h.To = &To{MustUnderstand: u.To.MustUnderstand, Value: u.To.Value}
	}
	if u.ReplyTo != nil {
		h.ReplyTo = &ReplyTo{
			Address: Address{MustUnderstand: u.ReplyTo.Address.MustUnderstand, Value: u.ReplyTo.Address.Value},
		}
	}
	if u.SelectorSet != nil {
		ss := &SelectorSet{}
		for _, s := range u.SelectorSet.Selectors {
			ss.Selectors = append(ss.Selectors, Selector(s))
		}
		h.SelectorSet = ss
	}
	return h
}

// --- Body ---

// Body は SOAP ボディを表す
type Body struct {
	Content []byte
}

type marshalBody struct {
	Content []byte `xml:",innerxml"`
}

type unmarshalBody struct {
	Content []byte `xml:",innerxml"`
}

// --- ヘッダー要素の公開型 ---

// Action は WS-Addressing Action ヘッダー
type Action struct {
	MustUnderstand string
	Value          string
}

// ResourceURI は WS-Man ResourceURI ヘッダー
type ResourceURI struct {
	MustUnderstand string
	Value          string
}

// MaxEnvelopeSize は WS-Man MaxEnvelopeSize ヘッダー
type MaxEnvelopeSize struct {
	MustUnderstand string
	Value          int
}

// OperationTimeout は WS-Man OperationTimeout ヘッダー
type OperationTimeout struct {
	Value string
}

// MessageID は WS-Addressing MessageID ヘッダー
type MessageID struct {
	Value string
}

// To は WS-Addressing To ヘッダー
type To struct {
	MustUnderstand string
	Value          string
}

// ReplyTo は WS-Addressing ReplyTo ヘッダー
type ReplyTo struct {
	Address Address
}

// Address は WS-Addressing Address 要素
type Address struct {
	MustUnderstand string
	Value          string
}

// Selector は SelectorSet 内の個別セレクタ
type Selector struct {
	Name  string
	Value string
}

// SelectorSet は WS-Man SelectorSet ヘッダー
type SelectorSet struct {
	Selectors []Selector
}

// --- Marshal 用の内部型 ---

type marshalAction struct {
	MustUnderstand string `xml:"s:mustUnderstand,attr,omitempty"`
	Value          string `xml:",chardata"`
}

type marshalResourceURI struct {
	MustUnderstand string `xml:"s:mustUnderstand,attr,omitempty"`
	Value          string `xml:",chardata"`
}

type marshalMaxEnvelopeSize struct {
	MustUnderstand string `xml:"s:mustUnderstand,attr,omitempty"`
	Value          int    `xml:",chardata"`
}

type marshalOperationTimeout struct {
	Value string `xml:",chardata"`
}

type marshalMessageID struct {
	Value string `xml:",chardata"`
}

type marshalTo struct {
	MustUnderstand string `xml:"s:mustUnderstand,attr,omitempty"`
	Value          string `xml:",chardata"`
}

type marshalReplyTo struct {
	Address marshalAddress `xml:"a:Address"`
}

type marshalAddress struct {
	MustUnderstand string `xml:"s:mustUnderstand,attr,omitempty"`
	Value          string `xml:",chardata"`
}

type marshalSelector struct {
	Name  string `xml:"Name,attr"`
	Value string `xml:",chardata"`
}

type marshalSelectorSet struct {
	Selectors []marshalSelector `xml:"w:Selector"`
}

// --- Unmarshal 用の内部型 ---

type unmarshalAction struct {
	MustUnderstand string `xml:"mustUnderstand,attr"`
	Value          string `xml:",chardata"`
}

type unmarshalResourceURI struct {
	MustUnderstand string `xml:"mustUnderstand,attr"`
	Value          string `xml:",chardata"`
}

type unmarshalMaxEnvelopeSize struct {
	MustUnderstand string `xml:"mustUnderstand,attr"`
	Value          int    `xml:",chardata"`
}

type unmarshalOperationTimeout struct {
	Value string `xml:",chardata"`
}

type unmarshalMessageID struct {
	Value string `xml:",chardata"`
}

type unmarshalTo struct {
	MustUnderstand string `xml:"mustUnderstand,attr"`
	Value          string `xml:",chardata"`
}

type unmarshalReplyTo struct {
	Address unmarshalAddress `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing Address"`
}

type unmarshalAddress struct {
	MustUnderstand string `xml:"mustUnderstand,attr"`
	Value          string `xml:",chardata"`
}

type unmarshalSelector struct {
	Name  string `xml:"Name,attr"`
	Value string `xml:",chardata"`
}

type unmarshalSelectorSet struct {
	Selectors []unmarshalSelector `xml:"http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd Selector"`
}

// --- Option パターン ---

// Option はエンベロープ構築時のオプション
type Option func(*Envelope)

// NewEnvelope は新しい SOAP エンベロープを作成する
func NewEnvelope(opts ...Option) *Envelope {
	env := &Envelope{}
	for _, opt := range opts {
		opt(env)
	}
	return env
}

// WithAction は Action ヘッダーを設定する
func WithAction(action string) Option {
	return func(env *Envelope) {
		env.Header.Action = &Action{
			MustUnderstand: "true",
			Value:          action,
		}
	}
}

// WithResourceURI は ResourceURI ヘッダーを設定する
func WithResourceURI(uri string) Option {
	return func(env *Envelope) {
		env.Header.ResourceURI = &ResourceURI{
			MustUnderstand: "true",
			Value:          uri,
		}
	}
}

// WithMaxEnvelopeSize は MaxEnvelopeSize ヘッダーを設定する
func WithMaxEnvelopeSize(size int) Option {
	return func(env *Envelope) {
		env.Header.MaxEnvelopeSize = &MaxEnvelopeSize{
			MustUnderstand: "true",
			Value:          size,
		}
	}
}

// WithOperationTimeout は OperationTimeout ヘッダーを設定する
func WithOperationTimeout(timeout string) Option {
	return func(env *Envelope) {
		env.Header.OperationTimeout = &OperationTimeout{
			Value: timeout,
		}
	}
}

// WithMessageID は MessageID ヘッダーを設定する
func WithMessageID(id string) Option {
	return func(env *Envelope) {
		env.Header.MessageID = &MessageID{
			Value: id,
		}
	}
}

// WithTo は To ヘッダーを設定する
func WithTo(to string) Option {
	return func(env *Envelope) {
		env.Header.To = &To{
			MustUnderstand: "true",
			Value:          to,
		}
	}
}

// WithReplyTo は ReplyTo ヘッダーを設定する
func WithReplyTo(address string) Option {
	return func(env *Envelope) {
		env.Header.ReplyTo = &ReplyTo{
			Address: Address{
				MustUnderstand: "true",
				Value:          address,
			},
		}
	}
}

// WithSelector は SelectorSet にセレクタを追加する
func WithSelector(name, value string) Option {
	return func(env *Envelope) {
		if env.Header.SelectorSet == nil {
			env.Header.SelectorSet = &SelectorSet{}
		}
		env.Header.SelectorSet.Selectors = append(env.Header.SelectorSet.Selectors, Selector{
			Name:  name,
			Value: value,
		})
	}
}

// SetBody はエンベロープのボディに XML コンテンツを設定する
func (env *Envelope) SetBody(content []byte) {
	env.Body.Content = content
}

// MarshalEnvelope は Envelope を SOAP XML バイト列にシリアライズする
func MarshalEnvelope(env *Envelope) ([]byte, error) {
	data, err := xml.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, err
	}
	result := make([]byte, 0, len(xml.Header)+len(data)+1)
	result = append(result, []byte(xml.Header)...)
	result = append(result, data...)
	result = append(result, '\n')
	return result, nil
}

// UnmarshalEnvelope は SOAP XML バイト列を Envelope にデシリアライズする
func UnmarshalEnvelope(data []byte) (*Envelope, error) {
	var env Envelope
	if err := xml.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	return &env, nil
}
