package hyperv

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Unmarshal は CIM プロパティ map を cim タグ付き構造体にマッピングする。
//
// 動作:
//   - cim タグなしのフィールドはスキップ
//   - props にないプロパティはゼロ値
//   - 型変換失敗で fail-fast にエラーを返す
//
// scalar 専用。配列フィールド ([]string 等) を含む構造体には UnmarshalList を使うこと。
func Unmarshal(props map[string]string, v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("Unmarshal: 引数は構造体への非 nil ポインタである必要があります")
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("Unmarshal: 引数は構造体ポインタである必要があります（got %s）", rv.Kind())
	}

	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("cim")
		if tag == "" {
			continue
		}
		raw, ok := props[tag]
		if !ok {
			continue
		}
		if err := setField(rv.Field(i), raw); err != nil {
			return fmt.Errorf("failed to unmarshal field %q (cim:%q): %w", field.Name, tag, err)
		}
	}
	return nil
}

// UnmarshalList は配列対応版の Unmarshal。
// CIM プロパティ map[string][]string を cim タグ付き構造体にマッピングする。
//
// scalar フィールド (string, uint16, ...): props[tag] の最初の要素を使う。
// slice フィールド ([]string, []uint16, ...): props[tag] の全要素を slice に展開。
//
// 配列フィールドを持たない既存構造体に対しても scalar 動作する。
func UnmarshalList(props map[string][]string, v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("UnmarshalList: 引数は構造体への非 nil ポインタである必要があります")
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("UnmarshalList: 引数は構造体ポインタである必要があります（got %s）", rv.Kind())
	}

	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("cim")
		if tag == "" {
			continue
		}
		values, ok := props[tag]
		if !ok || len(values) == 0 {
			continue
		}
		fv := rv.Field(i)
		if fv.Kind() == reflect.Slice {
			if err := setSliceField(fv, values); err != nil {
				return fmt.Errorf("failed to unmarshal field %q (cim:%q): %w", field.Name, tag, err)
			}
			continue
		}
		// scalar フィールドは最初の値を使う
		if err := setField(fv, values[0]); err != nil {
			return fmt.Errorf("failed to unmarshal field %q (cim:%q): %w", field.Name, tag, err)
		}
	}
	return nil
}

func setField(fv reflect.Value, raw string) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetUint(n)
	case reflect.Bool:
		// CIM の bool は "TRUE"/"FALSE"（大文字）が標準だが、大小文字を許容する。
		switch strings.ToUpper(raw) {
		case "TRUE":
			fv.SetBool(true)
		case "FALSE":
			fv.SetBool(false)
		default:
			return fmt.Errorf("invalid bool value: %q", raw)
		}
	default:
		return fmt.Errorf("unsupported field kind: %s", fv.Kind())
	}
	return nil
}

// setSliceField は要素型ごとに values を slice に変換して fv に格納する。
func setSliceField(fv reflect.Value, values []string) error {
	elemType := fv.Type().Elem()
	slice := reflect.MakeSlice(fv.Type(), len(values), len(values))
	for i, raw := range values {
		if err := setField(slice.Index(i), raw); err != nil {
			return fmt.Errorf("element [%d] (type %s): %w", i, elemType, err)
		}
	}
	fv.Set(slice)
	return nil
}
