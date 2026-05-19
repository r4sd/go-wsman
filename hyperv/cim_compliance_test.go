package hyperv

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// cim_compliance_test.go は Msvm_* struct の `cim:"..."` タグが
// Microsoft 公式 MOF (Managed Object Format) と整合するかを検証する。
//
// 設計方針 (案 D ハイブリッド・ミニマム):
//   - 新規 Msvm_* クラス追加時のみテスト必須 (TDD サイクルに組み込み)
//   - 既存クラスへの遡及は不要 (手動監査で確認済)
//   - 「がちがちにならない」設計: skipFields で意図的な逸脱を明示可能
//
// 詳細・経緯: Obsidian `40-Knowledge/cim/cim-bindings-mof-verification.md`
//
// 新規クラス追加時の手順:
//   1. testdata/mof/{class_snake_case}.txt に MOF プロパティを保存
//      形式: 1 行 1 プロパティ、空行と '#' コメント無視
//        InstanceID string
//        Format uint16
//        BootSourceOrder string[]
//      ポリシー: fixture には struct が cim タグで参照するプロパティのみ列挙する
//      (MOF 全プロパティ網羅は不要、保守負債回避のため)。
//   2. このファイルに Test 関数を追加 (TestCIMCompliance_MemorySettingData をテンプレに)
//   3. 必要なら skipFields に許容逸脱を明示 (通常は nil で OK)

// mofProperty は MOF fixture から読み取った CIM プロパティ。
type mofProperty struct {
	Name string // 例: "Format"
	Type string // 例: "uint16", "string", "string[]"
}

// loadMOFFixture は testdata/mof/{filename} から CIM プロパティリストを読み込む。
func loadMOFFixture(t *testing.T, filename string) []mofProperty {
	t.Helper()
	path := filepath.Join("testdata", "mof", filename)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("MOF fixture を開けません: %v", err)
	}
	defer f.Close()

	var props []mofProperty
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			t.Fatalf("MOF fixture %s: 不正な行 %q (期待: <Name> <Type>)", filename, line)
		}
		props = append(props, mofProperty{Name: fields[0], Type: fields[1]})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("MOF fixture スキャン失敗: %v", err)
	}
	return props
}

// goTypeToCIM は Go の reflect.Type を CIM 型表現に変換する。
// XML 経由で受信する場合 datetime は string で表現されるため、両者を string で同等扱いする。
//
// 未対応の型は空文字を返す (呼び出し側で明示的に Fatal させるため)。
func goTypeToCIM(rt reflect.Type) string {
	switch rt.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Uint16:
		return "uint16"
	case reflect.Uint32:
		return "uint32"
	case reflect.Uint64:
		return "uint64"
	case reflect.Slice:
		elem := goTypeToCIM(rt.Elem())
		if elem == "" {
			return ""
		}
		return elem + "[]"
	default:
		return ""
	}
}

// cimTypesCompatible は Go 型と MOF 型が許容範囲か判定する。
// XML 経由で受信する都合上、datetime は string で受け取るのが慣例。
func cimTypesCompatible(goType, mofType string) bool {
	if goType == mofType {
		return true
	}
	// datetime は string で受信 (XML 経由の慣例)
	if goType == "string" && mofType == "datetime" {
		return true
	}
	return false
}

// assertCIMCompliance は struct の cim タグが MOF fixture と整合するか検証する。
//
// fixture ポリシー: fixture には「struct が cim タグで参照するプロパティのみ」を列挙する
// (MOF 全プロパティ網羅は不要)。理由: Msvm_* クラスは 50+ プロパティを持つものが多く、
// 全コピーは fixture 肥大化と保守負債につながるため。
//
// 検出する問題:
//   - cim タグ値が MOF に存在しない (名前違い or fixture 漏れ or MOF 不在)
//   - Go 型が MOF 型と非互換 (型違い、配列性違反)
//
// 検出できない問題 (運用でカバー):
//   - fixture と現行 MOF の鮮度ずれ (新 Windows での MOF 更新)
//     → fixture コメントの `Fetched:` 日付を年 1 回程度確認、Obsidian ナレッジ参照
//
// 既知の許容逸脱は skipFields (フィールド名 → 理由) で除外可能。
// 通常は nil で OK。配列化保留中の HostResource 等の意図的な逸脱がある場合のみ指定する。
func assertCIMCompliance(t *testing.T, structVal interface{}, fixtureName string, skipFields map[string]string) {
	t.Helper()
	mofProps := loadMOFFixture(t, fixtureName)
	mofIndex := make(map[string]string, len(mofProps))
	for _, p := range mofProps {
		mofIndex[p.Name] = p.Type
	}

	rt := reflect.TypeOf(structVal)
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		t.Fatalf("assertCIMCompliance: 引数は構造体 (またはそのポインタ) である必要があります（got %s）", rt.Kind())
	}

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("cim")
		if tag == "" {
			continue
		}
		if reason, skip := skipFields[field.Name]; skip {
			t.Logf("skip %s.%s (cim:%q): %s", rt.Name(), field.Name, tag, reason)
			continue
		}

		mofType, ok := mofIndex[tag]
		if !ok {
			t.Errorf("%s.%s: cim タグ %q が MOF fixture (%s) に存在しません",
				rt.Name(), field.Name, tag, fixtureName)
			continue
		}

		goType := goTypeToCIM(field.Type)
		if goType == "" {
			t.Fatalf("%s.%s: 未対応の Go 型 %s。goTypeToCIM を拡張してください",
				rt.Name(), field.Name, field.Type)
		}
		if !cimTypesCompatible(goType, mofType) {
			t.Errorf("%s.%s (cim:%q): Go 型 %s が MOF 型 %s と非互換",
				rt.Name(), field.Name, tag, goType, mofType)
		}
	}
}

// TestCIMCompliance_MemorySettingData は Msvm_MemorySettingData の cim タグが MOF と整合するか検証する。
//
// 本テストは新規 Msvm_* クラス追加時のテンプレートとして機能する。
// 既存クラスの全網羅ではなく、テンプレ + 新規追加クラスのみ対象とする方針 (案 D)。
func TestCIMCompliance_MemorySettingData(t *testing.T) {
	assertCIMCompliance(t,
		&Msvm_MemorySettingData{},
		"msvm_memorysettingdata.txt",
		nil, // 許容逸脱なし
	)
}
