package hyperv

import (
	"strings"
	"testing"
)

// TestParseEmbeddedInstance は CIM-XML INSTANCE 形式 (実機 Hyper-V の出力) を
// map[string]string に変換できることを検証する。
//
// 入力は 2026-07-04 に実機 Hyper-V ホストの GetVirtualHardDiskSettingData 出力から
// 採取した実形式に基づく (旧テストは手書き WS-CIM ツリー形式で実機と乖離しており #89 の
// 遠因になった)。
func TestParseEmbeddedInstance(t *testing.T) {
	t.Run("INSTANCE形式(実機)のパース", func(t *testing.T) {
		xml := `<INSTANCE CLASSNAME="Msvm_VirtualHardDiskSettingData">` +
			`<PROPERTY NAME="Path" TYPE="string"><VALUE>D:\Hyper-V\vm.vhdx</VALUE></PROPERTY>` +
			`<PROPERTY NAME="MaxInternalSize" TYPE="uint64"><VALUE>53687091200</VALUE></PROPERTY>` +
			`<PROPERTY NAME="Format" TYPE="uint16"><VALUE>3</VALUE></PROPERTY>` +
			`</INSTANCE>`

		got, err := parseEmbeddedInstance(xml)
		if err != nil {
			t.Fatalf("parseEmbeddedInstance: %v", err)
		}
		if got["Path"] != `D:\Hyper-V\vm.vhdx` {
			t.Errorf("Path: got %q", got["Path"])
		}
		if got["MaxInternalSize"] != "53687091200" {
			t.Errorf("MaxInternalSize: got %q", got["MaxInternalSize"])
		}
		if got["Format"] != "3" {
			t.Errorf("Format: got %q", got["Format"])
		}
	})

	t.Run("VALUE無し/空のプロパティは空文字", func(t *testing.T) {
		// DataAlignment は PROPAGATED で VALUE 無し、ParentPath は空 VALUE (実機で出る形)。
		xml := `<INSTANCE CLASSNAME="X">` +
			`<PROPERTY NAME="Path" TYPE="string"><VALUE>D:\a.vhdx</VALUE></PROPERTY>` +
			`<PROPERTY NAME="DataAlignment" PROPAGATED="true" TYPE="uint64"></PROPERTY>` +
			`<PROPERTY NAME="ParentPath" TYPE="string"><VALUE></VALUE></PROPERTY>` +
			`</INSTANCE>`

		got, err := parseEmbeddedInstance(xml)
		if err != nil {
			t.Fatalf("parseEmbeddedInstance: %v", err)
		}
		if got["Path"] != `D:\a.vhdx` {
			t.Errorf("Path: got %q", got["Path"])
		}
		if got["DataAlignment"] != "" {
			t.Errorf("DataAlignment: got %q, want empty", got["DataAlignment"])
		}
		if got["ParentPath"] != "" {
			t.Errorf("ParentPath: got %q, want empty", got["ParentPath"])
		}
	})

	t.Run("marshal→parse 往復", func(t *testing.T) {
		// marshalEmbeddedInstance の出力 (CDATA 除去後) を parse で戻せること = 対称性。
		type sd struct {
			Path            string `cim:"Path"`
			MaxInternalSize uint64 `cim:"MaxInternalSize"`
			Format          uint16 `cim:"Format"`
		}
		in := &sd{Path: `D:\round.vhdx`, MaxInternalSize: 12345, Format: 3}
		marshaled, err := marshalEmbeddedInstance(in, "Msvm_VirtualHardDiskSettingData", "")
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// CDATA ラッパを外して INSTANCE ツリーだけ取り出す。
		inner := strings.TrimSuffix(strings.TrimPrefix(marshaled, "<![CDATA["), "]]>")
		got, err := parseEmbeddedInstance(inner)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if got["Path"] != `D:\round.vhdx` || got["MaxInternalSize"] != "12345" || got["Format"] != "3" {
			t.Errorf("往復不一致: %+v", got)
		}
	})

	t.Run("不正な XML はエラー", func(t *testing.T) {
		_, err := parseEmbeddedInstance(`not an xml`)
		if err == nil {
			t.Fatal("expected error for invalid XML")
		}
	})
}

// TestMarshalEmbeddedInstance は struct を CIM-XML EmbeddedInstance に変換し、
// CDATA で包んで返すことを検証する。
//
// MS WS-Man の string パラメータは CIM-XML の <INSTANCE CLASSNAME=...> 形式
// (DSP0201) を CDATA でエスケープして要求する。WS-CIM 要素ツリー形式 (旧実装) は
// SchemaValidationError になるため #81 で本形式に修正した。
func TestMarshalEmbeddedInstance(t *testing.T) {
	type vssd struct {
		ElementName          string `cim:"ElementName"`
		VirtualSystemSubType string `cim:"VirtualSystemSubType"`
	}
	s := vssd{
		ElementName:          "tf-test",
		VirtualSystemSubType: "Microsoft:Hyper-V:SubType:2",
	}

	got, err := marshalEmbeddedInstance(&s, "Msvm_VirtualSystemSettingData", nsVirtV2+"/Msvm_VirtualSystemSettingData")
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}

	// 戻り値は CDATA で包まれた CIM-XML INSTANCE ツリー。
	want := `<![CDATA[<INSTANCE CLASSNAME="Msvm_VirtualSystemSettingData">` +
		`<PROPERTY NAME="ElementName" TYPE="string"><VALUE>tf-test</VALUE></PROPERTY>` +
		`<PROPERTY NAME="VirtualSystemSubType" TYPE="string"><VALUE>Microsoft:Hyper-V:SubType:2</VALUE></PROPERTY>` +
		`</INSTANCE>]]>`
	if got != want {
		t.Errorf("marshalEmbeddedInstance output mismatch:\n got: %s\nwant: %s", got, want)
	}
}

// TestMarshalEmbeddedInstance_CDATAWrap は戻り値が CDATA で包まれること、および
// 値中に "]]>" を含む場合に CDATA が安全に分割エスケープされることを検証する。
func TestMarshalEmbeddedInstance_CDATAWrap(t *testing.T) {
	type settings struct {
		ElementName string `cim:"ElementName"`
	}
	// "]]>" を含む値 ('>' は xmlEscape で &gt; になるため、ここでは無害化経路の
	// 確認のため cdataWrap を直接検証する)。
	got := cdataWrap(`a]]>b`)
	want := `<![CDATA[a]]]]><![CDATA[>b]]>`
	if got != want {
		t.Errorf("cdataWrap mismatch:\n got: %s\nwant: %s", got, want)
	}

	s := settings{ElementName: "vm"}
	out, err := marshalEmbeddedInstance(&s, "Msvm_Test", "ns")
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}
	if !strings.HasPrefix(out, "<![CDATA[") || !strings.HasSuffix(out, "]]>") {
		t.Errorf("output should be CDATA-wrapped, got: %s", out)
	}
}

// TestMarshalEmbeddedInstance_CIMTypes は各 Go 型が CIM 型名 (TYPE 属性) に
// 正しくマッピングされることを検証する。
func TestMarshalEmbeddedInstance_CIMTypes(t *testing.T) {
	type allTypes struct {
		S   string `cim:"S"`
		U16 uint16 `cim:"U16"`
		U32 uint32 `cim:"U32"`
		U64 uint64 `cim:"U64"`
		B   bool   `cim:"B"`
	}
	s := allTypes{S: "x", U16: 16, U32: 32, U64: 64, B: true}

	got, err := marshalEmbeddedInstance(&s, "Msvm_Test", "ns")
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}

	wants := []string{
		`<PROPERTY NAME="S" TYPE="string"><VALUE>x</VALUE></PROPERTY>`,
		`<PROPERTY NAME="U16" TYPE="uint16"><VALUE>16</VALUE></PROPERTY>`,
		`<PROPERTY NAME="U32" TYPE="uint32"><VALUE>32</VALUE></PROPERTY>`,
		`<PROPERTY NAME="U64" TYPE="uint64"><VALUE>64</VALUE></PROPERTY>`,
		// bool は CIM-XML 標準 (DSP0201) の小文字 true/false。
		`<PROPERTY NAME="B" TYPE="boolean"><VALUE>true</VALUE></PROPERTY>`,
	}
	for _, want := range wants {
		if !contains(got, want) {
			t.Errorf("XML should contain %q, got: %s", want, got)
		}
	}
}

// TestMarshalEmbeddedInstance_StringSlice は []string フィールドが
// CIM-XML の PROPERTY.ARRAY / VALUE.ARRAY 形式に展開されることを検証する。
func TestMarshalEmbeddedInstance_StringSlice(t *testing.T) {
	type settings struct {
		ElementName string   `cim:"ElementName"`
		Notes       []string `cim:"Notes"`
	}
	s := settings{
		ElementName: "my-vm",
		Notes:       []string{"line1", "line2", "line3"},
	}

	got, err := marshalEmbeddedInstance(&s, "Msvm_VirtualSystemSettingData", nsVirtV2+"/Msvm_VirtualSystemSettingData")
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}

	wantArray := `<PROPERTY.ARRAY NAME="Notes" TYPE="string"><VALUE.ARRAY>` +
		`<VALUE>line1</VALUE><VALUE>line2</VALUE><VALUE>line3</VALUE>` +
		`</VALUE.ARRAY></PROPERTY.ARRAY>`
	if !contains(got, wantArray) {
		t.Errorf("XML should contain array property %q, got: %s", wantArray, got)
	}
	if !contains(got, `<PROPERTY NAME="ElementName" TYPE="string"><VALUE>my-vm</VALUE></PROPERTY>`) {
		t.Errorf("ElementName missing in XML: %s", got)
	}
}

// TestMarshalEmbeddedInstance_Uint16Slice は []uint16 配列の TYPE 属性と展開を検証する。
func TestMarshalEmbeddedInstance_Uint16Slice(t *testing.T) {
	type settings struct {
		Ports []uint16 `cim:"Ports"`
	}
	s := settings{Ports: []uint16{22, 80, 443}}

	got, err := marshalEmbeddedInstance(&s, "Msvm_Test", "ns")
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}
	want := `<PROPERTY.ARRAY NAME="Ports" TYPE="uint16"><VALUE.ARRAY>` +
		`<VALUE>22</VALUE><VALUE>80</VALUE><VALUE>443</VALUE>` +
		`</VALUE.ARRAY></PROPERTY.ARRAY>`
	if !contains(got, want) {
		t.Errorf("XML should contain %q, got: %s", want, got)
	}
}

// TestMarshalEmbeddedInstance_OmitsEmptySlice は nil/空 slice が出力されないことを検証する。
// CIM SettingData ではゼロ値 = デフォルトの慣習を slice にも適用する。
func TestMarshalEmbeddedInstance_OmitsEmptySlice(t *testing.T) {
	type settings struct {
		ElementName string   `cim:"ElementName"`
		Notes       []string `cim:"Notes"`
		Empty       []string `cim:"Empty"`
	}
	s := settings{
		ElementName: "vm",
		Notes:       nil, // nil slice
		Empty:       []string{},
	}

	got, err := marshalEmbeddedInstance(&s, "Msvm_Test", "ns")
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}
	if contains(got, `NAME="Notes"`) {
		t.Errorf("nil slice should be omitted, got: %s", got)
	}
	if contains(got, `NAME="Empty"`) {
		t.Errorf("empty slice should be omitted, got: %s", got)
	}
}

// TestMarshalEmbeddedInstance_OmitsZeroValues はゼロ値フィールドが出力されないことを検証する。
// CIM の SettingData では未指定 = デフォルト適用なので、ゼロ値を送ると意図しない上書きになる。
func TestMarshalEmbeddedInstance_OmitsZeroValues(t *testing.T) {
	settings := Msvm_VirtualHardDiskSettingData{
		Path:            `D:\a.vhdx`,
		MaxInternalSize: 1073741824,
		// VirtualDiskFormat / Type / BlockSize 等は未設定（ゼロ値）
	}

	got, err := marshalEmbeddedInstance(&settings, "Msvm_VirtualHardDiskSettingData", nsVirtV2+"/Msvm_VirtualHardDiskSettingData")
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}

	// 設定したフィールドは含まれる
	if !contains(got, `NAME="Path"`) {
		t.Errorf("Path missing: %s", got)
	}
	if !contains(got, `NAME="MaxInternalSize"`) {
		t.Errorf("MaxInternalSize missing: %s", got)
	}
	// ゼロ値のフィールドは含まれない
	if contains(got, `NAME="Format"`) {
		t.Errorf("Format should be omitted (zero value), got: %s", got)
	}
	if contains(got, `NAME="BlockSize"`) {
		t.Errorf("BlockSize should be omitted (zero value), got: %s", got)
	}
}

// TestMarshalEmbeddedInstance_EscapesValues は VALUE 内の特殊文字が XML エスケープされることを検証する。
func TestMarshalEmbeddedInstance_EscapesValues(t *testing.T) {
	type settings struct {
		ElementName string `cim:"ElementName"`
	}
	s := settings{ElementName: `a<b>&"c`}

	got, err := marshalEmbeddedInstance(&s, "Msvm_Test", "ns")
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}
	// '<' '>' '&' は実体参照に変換される (CDATA 化は invoke 層の責務)。
	if !contains(got, `<VALUE>a&lt;b&gt;&amp;`) {
		t.Errorf("special chars should be XML-escaped, got: %s", got)
	}
}
