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

// marshalEmbeddedInstance は cim タグ付き構造体を CIM EmbeddedInstance XML に変換する。
//
// ゼロ値のフィールドは出力に含めない（CIM SettingData の慣習で未指定 = デフォルト）。
//
// 出力形式:
//
//	<p:ClassName xmlns:p="namespace">
//	  <p:Field1>value1</p:Field1>
//	  ...
//	</p:ClassName>
func marshalEmbeddedInstance(v interface{}, className, namespace string) (string, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return "", fmt.Errorf("marshalEmbeddedInstance: 引数は構造体への非 nil ポインタ")
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return "", fmt.Errorf("marshalEmbeddedInstance: 引数は構造体ポインタ（got %s）", rv.Kind())
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, `<p:%s xmlns:p=%q>`, className, namespace)

	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("cim")
		if tag == "" {
			continue
		}
		fv := rv.Field(i)
		// slice は同名要素の繰り返しに展開 (CIM 配列の慣習)。
		// nil/空 slice はゼロ値扱いで出力しない。
		if fv.Kind() == reflect.Slice {
			if fv.Len() == 0 {
				continue
			}
			for j := 0; j < fv.Len(); j++ {
				val, err := stringify(fv.Index(j))
				if err != nil {
					return "", fmt.Errorf("field %q [%d]: %w", field.Name, j, err)
				}
				fmt.Fprintf(&sb, "<p:%s>%s</p:%s>", tag, xmlEscape(val), tag)
			}
			continue
		}
		if fv.IsZero() {
			continue
		}
		val, err := stringify(fv)
		if err != nil {
			return "", fmt.Errorf("field %q: %w", field.Name, err)
		}
		fmt.Fprintf(&sb, "<p:%s>%s</p:%s>", tag, xmlEscape(val), tag)
	}

	fmt.Fprintf(&sb, "</p:%s>", className)
	return sb.String(), nil
}

func stringify(fv reflect.Value) (string, error) {
	switch fv.Kind() {
	case reflect.String:
		return fv.String(), nil
	case reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(fv.Uint(), 10), nil
	case reflect.Int, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(fv.Int(), 10), nil
	case reflect.Bool:
		if fv.Bool() {
			return "TRUE", nil
		}
		return "FALSE", nil
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
