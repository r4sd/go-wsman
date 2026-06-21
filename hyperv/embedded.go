package hyperv

import (
	"encoding/xml"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// parseEmbeddedInstance は CIM EmbeddedInstance の XML 文字列を
// プロパティ map に変換する。
//
// 入力形式（namespace prefix は任意）:
//
//	<p:ClassName xmlns:p="...">
//	  <p:Property1>value1</p:Property1>
//	  <p:Property2>value2</p:Property2>
//	</p:ClassName>
func parseEmbeddedInstance(xmlStr string) (map[string]string, error) {
	props := make(map[string]string)
	dec := xml.NewDecoder(strings.NewReader(xmlStr))

	var depth int
	var currentKey string
	var currentValue strings.Builder

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 {
				currentKey = t.Name.Local
				currentValue.Reset()
			}
		case xml.EndElement:
			if depth == 2 && currentKey != "" {
				props[currentKey] = currentValue.String()
				currentKey = ""
			}
			depth--
		case xml.CharData:
			if depth == 2 && currentKey != "" {
				currentValue.Write(t)
			}
		}
	}

	if len(props) == 0 {
		return nil, fmt.Errorf("parseEmbeddedInstance: no properties found in %q", xmlStr)
	}
	return props, nil
}

// marshalEmbeddedInstance は cim タグ付き構造体を CIM-XML の EmbeddedInstance に変換し、
// SOAP の string パラメータにそのまま埋め込める CDATA セクションとして返す。
//
// MS WS-Man の string パラメータ (DefineSystem の SystemSettings 等) は、CIM-XML
// (DSP0201) の <INSTANCE CLASSNAME=...> 形式を CDATA でエスケープして入れることを要求
// する。WS-CIM の要素ツリー形式 (namespace 付き <p:ClassName>...) は実機 Hyper-V で
// SchemaValidationError になる (#81)。libvirt の hypervSerializeEmbeddedParam と同じ
// 形式・CDATA 化を採用する。
//
// CDATA でラップするのは、EPR REF パラメータ (生 XML ツリーのまま埋め込む) と
// embedded instance を wsman 層で区別せず、どちらも raw 挿入できるようにするため。
// 戻り値は既に CDATA 済みなので、呼び出し側 (Invoke/InvokeMulti) は値をそのまま
// パラメータに渡せばよい。
//
// namespace 引数は CIM-XML 形式では使わないが、呼び出し側との互換のため残している
// (将来的に削除可)。
//
// ゼロ値のフィールドは出力に含めない（CIM SettingData の慣習で未指定 = デフォルト）。
//
// 出力形式 (CDATA で包まれる前の INSTANCE ツリー):
//
//	<INSTANCE CLASSNAME="ClassName">
//	  <PROPERTY NAME="Field1" TYPE="string"><VALUE>value1</VALUE></PROPERTY>
//	  <PROPERTY.ARRAY NAME="Arr" TYPE="string"><VALUE.ARRAY><VALUE>a</VALUE>...</VALUE.ARRAY></PROPERTY.ARRAY>
//	  ...
//	</INSTANCE>
func marshalEmbeddedInstance(v interface{}, className, _ string) (string, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return "", fmt.Errorf("marshalEmbeddedInstance: 引数は構造体への非 nil ポインタ")
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return "", fmt.Errorf("marshalEmbeddedInstance: 引数は構造体ポインタ（got %s）", rv.Kind())
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, `<INSTANCE CLASSNAME=%q>`, className)

	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("cim")
		if tag == "" {
			continue
		}
		fv := rv.Field(i)
		// slice は PROPERTY.ARRAY / VALUE.ARRAY に展開 (CIM-XML 配列の慣習)。
		// nil/空 slice はゼロ値扱いで出力しない。
		if fv.Kind() == reflect.Slice {
			if fv.Len() == 0 {
				continue
			}
			cimType, err := cimTypeName(fv.Type().Elem().Kind())
			if err != nil {
				return "", fmt.Errorf("field %q: %w", field.Name, err)
			}
			fmt.Fprintf(&sb, `<PROPERTY.ARRAY NAME=%q TYPE=%q><VALUE.ARRAY>`, tag, cimType)
			for j := 0; j < fv.Len(); j++ {
				val, err := stringify(fv.Index(j))
				if err != nil {
					return "", fmt.Errorf("field %q [%d]: %w", field.Name, j, err)
				}
				fmt.Fprintf(&sb, "<VALUE>%s</VALUE>", xmlEscape(val))
			}
			sb.WriteString(`</VALUE.ARRAY></PROPERTY.ARRAY>`)
			continue
		}
		if fv.IsZero() {
			continue
		}
		cimType, err := cimTypeName(fv.Kind())
		if err != nil {
			return "", fmt.Errorf("field %q: %w", field.Name, err)
		}
		val, err := stringify(fv)
		if err != nil {
			return "", fmt.Errorf("field %q: %w", field.Name, err)
		}
		fmt.Fprintf(&sb, `<PROPERTY NAME=%q TYPE=%q><VALUE>%s</VALUE></PROPERTY>`, tag, cimType, xmlEscape(val))
	}

	sb.WriteString(`</INSTANCE>`)
	return cdataWrap(sb.String()), nil
}

// cdataWrap は s を CDATA セクションで包む。s が CDATA 終端シーケンス "]]>" を
// 含む場合は、その境界で CDATA を分割して無害化する (XML の標準回避策)。
func cdataWrap(s string) string {
	// "]]>" を "]]" + ">" の境界で分割: 「...]]]]><![CDATA[>...」となり、
	// パーサからは元の "]]>" として復元される。
	safe := strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
	return "<![CDATA[" + safe + "]]>"
}

// cimTypeName は Go の reflect.Kind を CIM-XML の TYPE 属性値 (DSP0004 の CIM 型名) に
// マッピングする。PROPERTY/PROPERTY.ARRAY 要素の TYPE 属性は必須。
func cimTypeName(k reflect.Kind) (string, error) {
	switch k {
	case reflect.String:
		return "string", nil
	case reflect.Uint8:
		return "uint8", nil
	case reflect.Uint16:
		return "uint16", nil
	case reflect.Uint32:
		return "uint32", nil
	case reflect.Uint64:
		return "uint64", nil
	case reflect.Bool:
		return "boolean", nil
	default:
		return "", fmt.Errorf("unsupported field kind for CIM type: %s", k)
	}
}

func stringify(fv reflect.Value) (string, error) {
	switch fv.Kind() {
	case reflect.String:
		return fv.String(), nil
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(fv.Uint(), 10), nil
	case reflect.Int, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(fv.Int(), 10), nil
	case reflect.Bool:
		// CIM-XML 標準 (DSP0201) の boolean 値は小文字 true/false。
		if fv.Bool() {
			return "true", nil
		}
		return "false", nil
	default:
		return "", fmt.Errorf("unsupported field kind: %s", fv.Kind())
	}
}

// xmlEscape は要素テキスト内の特殊文字を XML エスケープする。
// バックスラッシュやコロン等のファイルパス文字はエスケープ不要。
func xmlEscape(s string) string {
	var sb strings.Builder
	if err := xml.EscapeText(&sb, []byte(s)); err != nil {
		return s
	}
	return sb.String()
}
