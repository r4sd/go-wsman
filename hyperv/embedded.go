package hyperv

import (
	"encoding/xml"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// parseEmbeddedInstance は CIM-XML EmbeddedInstance の XML 文字列を
// プロパティ map に変換する。marshalEmbeddedInstance の逆変換で、実機 Hyper-V が
// GetVirtualHardDiskSettingData 等で返す出力形式と対称。
//
// 入力形式 (DSP0201 CIM-XML の INSTANCE ツリー):
//
//	<INSTANCE CLASSNAME="ClassName">
//	  <PROPERTY NAME="Prop1" TYPE="string"><VALUE>value1</VALUE></PROPERTY>
//	  <PROPERTY NAME="Prop2" TYPE="uint16"><VALUE>3</VALUE></PROPERTY>
//	  <PROPERTY.ARRAY NAME="Arr" TYPE="string"><VALUE.ARRAY><VALUE>a</VALUE>...</VALUE.ARRAY></PROPERTY.ARRAY>
//	</INSTANCE>
//
// キーは PROPERTY の NAME 属性、値は入れ子の VALUE のテキストの配列。
// 配列 (PROPERTY.ARRAY) の複数 VALUE は要素ごとに保持する。
//
// VALUE 要素が 1 つも無いプロパティ (PROPAGATED 等) は **キーごと作らない**
// (wsman の parseInstances と同じ意味論)。空の VALUE (<VALUE></VALUE>) は
// 1 要素の空文字として保持する。
//
// PROPERTY のネスト (要素ツリー形式の embedded object) は **エラーにする** (#93)。
// この parser はフラットな状態しか持たないため、内側 PROPERTY の EndElement が
// 外側の状態を消し、外側プロパティと後続の兄弟が silent に破損する。実機 Hyper-V の
// 応答では未観測なので、未検証のネスト解析を書くより落ちて気付ける方を選ぶ。
//
// 以前は map[string]string を返し複数 VALUE を **連結** していたため、配列プロパティが
// 静かに壊れていた ("a","b" → "ab")。UnmarshalList への一本化に合わせて修正した。
func parseEmbeddedInstance(xmlStr string) (map[string][]string, error) {
	props := make(map[string][]string)
	dec := xml.NewDecoder(strings.NewReader(xmlStr))

	var curName string // 現在の PROPERTY / PROPERTY.ARRAY の NAME
	var inValue bool   // <VALUE> の内側か
	var haveProp bool  // 有効な PROPERTY を処理中か
	var inProp bool    // PROPERTY / PROPERTY.ARRAY の内側か (NAME の有無に依らない)
	var val strings.Builder

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "PROPERTY", "PROPERTY.ARRAY":
				if inProp {
					return nil, fmt.Errorf(
						"parseEmbeddedInstance: プロパティ %q の内側にネストした %s (NAME=%q) がある。要素ツリー形式の embedded object は未対応",
						curName, t.Name.Local, attrValue(t.Attr, "NAME"))
				}
				inProp = true
				curName = attrValue(t.Attr, "NAME")
				val.Reset()
				haveProp = curName != ""
			case "INSTANCE":
				if inProp {
					return nil, fmt.Errorf(
						"parseEmbeddedInstance: プロパティ %q の内側にネストした INSTANCE (CLASSNAME=%q) がある。要素ツリー形式の embedded object は未対応",
						curName, attrValue(t.Attr, "CLASSNAME"))
				}
			case "VALUE":
				inValue = true
			}
		case xml.CharData:
			if inValue {
				val.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "VALUE":
				inValue = false
				if haveProp {
					props[curName] = append(props[curName], val.String())
					val.Reset()
				}
			case "PROPERTY", "PROPERTY.ARRAY":
				// VALUE が 1 つも無いプロパティ (PROPAGATED 等) はキーごと作らない。
				// wsman 側の parseInstances / extractProperties と同じ意味論に揃える。
				//
				// 1 要素の空文字を入れると、空の PROPERTY.ARRAY が [""] になり
				// []uint16 フィールドで ParseUint("") エラー、[]string フィールドで
				// 「長さ 1 の空要素」という誤った値になる (批判的レビュー指摘)。
				curName = ""
				haveProp = false
				inProp = false
			}
		}
	}

	if len(props) == 0 {
		return nil, fmt.Errorf("parseEmbeddedInstance: no properties found in %q", xmlStr)
	}
	return props, nil
}

// attrValue は XML 属性リストから指定名 (大文字小文字無視) の値を返す。
func attrValue(attrs []xml.Attr, name string) string {
	for _, a := range attrs {
		if strings.EqualFold(a.Name.Local, name) {
			return a.Value
		}
	}
	return ""
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
		tag, isDatetime := parseCimTag(field.Tag.Get("cim"))
		if tag == "" {
			continue
		}
		if err := marshalField(&sb, rv.Field(i), field.Name, tag, isDatetime); err != nil {
			return "", err
		}
	}

	sb.WriteString(`</INSTANCE>`)
	return cdataWrap(sb.String()), nil
}

// marshalField は 1 フィールドを PROPERTY / PROPERTY.ARRAY として sb に書く。
// 出力しない (ゼロ値 / nil ポインタ / 空 slice) 場合は何も書かずに nil を返す。
func marshalField(sb *strings.Builder, fv reflect.Value, fieldName, tag string, isDatetime bool) error {
	// slice は PROPERTY.ARRAY / VALUE.ARRAY に展開 (CIM-XML 配列の慣習)。
	// nil/空 slice はゼロ値扱いで出力しない。
	if fv.Kind() == reflect.Slice {
		return marshalSliceField(sb, fv, fieldName, tag, isDatetime)
	}
	// ポインタフィールドは「送る/送らない」を呼び出し側が明示する (#135)。
	//
	//	nil    = 送らない (値型のゼロ値スキップと同じ「変更しない」)
	//	&false = 明示的に false を送る
	//
	// 値型だと false / 0 / "" がゼロ値スキップに掛かり、「変更しない」と
	// 「ゼロ値に変える」を区別する手段が無かった。ゼロ値が意味を持つフィールドだけ
	// 順次ポインタへ移す (最小インスタンスの原則は値型側でそのまま残る)。
	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			return nil
		}
		fv = fv.Elem()
	} else if fv.IsZero() {
		return nil
	}
	cimType, err := cimTypeName(fv.Kind())
	if err != nil {
		return fmt.Errorf("field %q: %w", fieldName, err)
	}
	val, err := stringify(fv)
	if err != nil {
		return fmt.Errorf("field %q: %w", fieldName, err)
	}
	// datetime は TYPE 属性だけでなく **値の書式も** read と非対称 (#119)。
	// ISO 8601 のまま TYPE="datetime" で送っても ErrorCode=32768 になる。
	if isDatetime {
		cimType = "datetime"
		if val, err = iso8601ToCIMInterval(val); err != nil {
			return fmt.Errorf("field %q: %w", fieldName, err)
		}
	}
	fmt.Fprintf(sb, `<PROPERTY NAME=%q TYPE=%q><VALUE>%s</VALUE></PROPERTY>`, tag, cimType, xmlEscape(val))
	return nil
}

// marshalSliceField は slice フィールドを PROPERTY.ARRAY として書く。
func marshalSliceField(sb *strings.Builder, fv reflect.Value, fieldName, tag string, isDatetime bool) error {
	if fv.Len() == 0 {
		return nil
	}
	// datetime の配列プロパティは実機にも MOF fixture にも存在しない。
	// 未検証の変換コードを置くより、要求されたら落とす方が安全
	// (黙って誤った書式で送ると ErrorCode=32768 になるだけで気付けない)。
	if isDatetime {
		return fmt.Errorf("field %q: datetime 配列は未対応 (実機に該当プロパティが無く未検証)", fieldName)
	}
	cimType, err := cimTypeName(fv.Type().Elem().Kind())
	if err != nil {
		return fmt.Errorf("field %q: %w", fieldName, err)
	}
	fmt.Fprintf(sb, `<PROPERTY.ARRAY NAME=%q TYPE=%q><VALUE.ARRAY>`, tag, cimType)
	for j := 0; j < fv.Len(); j++ {
		val, err := stringify(fv.Index(j))
		if err != nil {
			return fmt.Errorf("field %q [%d]: %w", fieldName, j, err)
		}
		fmt.Fprintf(sb, "<VALUE>%s</VALUE>", xmlEscape(val))
	}
	sb.WriteString(`</VALUE.ARRAY></PROPERTY.ARRAY>`)
	return nil
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
