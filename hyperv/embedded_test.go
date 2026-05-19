package hyperv

import (
	"testing"
)

// TestParseEmbeddedInstance は CIM EmbeddedInstance XML 文字列を
// map[string]string に変換できることを検証する。
func TestParseEmbeddedInstance(t *testing.T) {
	t.Run("単純な要素のパース", func(t *testing.T) {
		xml := `<p:Msvm_VirtualHardDiskSettingData xmlns:p="http://schemas.microsoft.com/wbem/wsman/1/wmi/root/virtualization/v2/Msvm_VirtualHardDiskSettingData"><p:Path>C:\vm.vhdx</p:Path><p:MaxInternalSize>10737418240</p:MaxInternalSize><p:Format>3</p:Format></p:Msvm_VirtualHardDiskSettingData>`

		got, err := parseEmbeddedInstance(xml)
		if err != nil {
			t.Fatalf("parseEmbeddedInstance: %v", err)
		}
		if got["Path"] != `C:\vm.vhdx` {
			t.Errorf("Path: got %q", got["Path"])
		}
		if got["MaxInternalSize"] != "10737418240" {
			t.Errorf("MaxInternalSize: got %q", got["MaxInternalSize"])
		}
		if got["Format"] != "3" {
			t.Errorf("Format: got %q", got["Format"])
		}
	})

	t.Run("空要素を含むパース", func(t *testing.T) {
		xml := `<p:Msvm_VirtualHardDiskSettingData xmlns:p="ns"><p:Path>C:\a.vhdx</p:Path><p:ParentPath/></p:Msvm_VirtualHardDiskSettingData>`

		got, err := parseEmbeddedInstance(xml)
		if err != nil {
			t.Fatalf("parseEmbeddedInstance: %v", err)
		}
		if got["Path"] != `C:\a.vhdx` {
			t.Errorf("Path: got %q", got["Path"])
		}
		if got["ParentPath"] != "" {
			t.Errorf("ParentPath: got %q, want empty string", got["ParentPath"])
		}
	})

	t.Run("不正な XML はエラー", func(t *testing.T) {
		_, err := parseEmbeddedInstance(`not an xml`)
		if err == nil {
			t.Fatal("expected error for invalid XML")
		}
	})
}

// TestMarshalEmbeddedInstance は struct を CIM EmbeddedInstance XML に変換できることを検証する。
func TestMarshalEmbeddedInstance(t *testing.T) {
	settings := Msvm_VirtualHardDiskSettingData{
		VirtualDiskFormat: VHDFormatVHDX,
		VirtualDiskType:   VHDTypeDynamic,
		Path:              `C:\VMs\new.vhdx`,
		MaxInternalSize:   10737418240,
	}

	got, err := marshalEmbeddedInstance(&settings, "Msvm_VirtualHardDiskSettingData", nsVirtV2+"/Msvm_VirtualHardDiskSettingData")
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}

	// 出力 XML を再パースして round-trip を確認
	parsed, err := parseEmbeddedInstance(got)
	if err != nil {
		t.Fatalf("re-parse failed: %v\nXML: %s", err, got)
	}
	if parsed["Path"] != `C:\VMs\new.vhdx` {
		t.Errorf("Path round-trip: got %q", parsed["Path"])
	}
	if parsed["MaxInternalSize"] != "10737418240" {
		t.Errorf("MaxInternalSize round-trip: got %q", parsed["MaxInternalSize"])
	}
	if parsed["Format"] != "3" {
		t.Errorf("Format round-trip: got %q", parsed["Format"])
	}
	if parsed["Type"] != "3" {
		t.Errorf("Type round-trip: got %q", parsed["Type"])
	}
}

// TestMarshalEmbeddedInstance_StringSlice は []string フィールドが
// 同名要素の繰り返しに展開されることを検証する。
// 例: Notes: []string{"a","b"} → <p:Notes>a</p:Notes><p:Notes>b</p:Notes>
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

	// 出力 XML 内に <p:Notes> が3つ含まれていることを確認
	cnt := 0
	for i := 0; i+len("<p:Notes>") <= len(got); i++ {
		if got[i:i+len("<p:Notes>")] == "<p:Notes>" {
			cnt++
		}
	}
	if cnt != 3 {
		t.Errorf("expected 3 <p:Notes> elements, got %d. XML: %s", cnt, got)
	}

	// 個別の値が含まれていることを確認
	for _, want := range []string{">line1<", ">line2<", ">line3<"} {
		if !contains(got, want) {
			t.Errorf("XML should contain %q, got: %s", want, got)
		}
	}
	if !contains(got, ">my-vm<") {
		t.Errorf("ElementName missing in XML: %s", got)
	}
}

// TestMarshalEmbeddedInstance_Uint16Slice は []uint16 フィールドの展開を検証する。
func TestMarshalEmbeddedInstance_Uint16Slice(t *testing.T) {
	type settings struct {
		Ports []uint16 `cim:"Ports"`
	}
	s := settings{Ports: []uint16{22, 80, 443}}

	got, err := marshalEmbeddedInstance(&s, "Msvm_Test", "ns")
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}
	for _, want := range []string{">22<", ">80<", ">443<"} {
		if !contains(got, want) {
			t.Errorf("XML should contain %q, got: %s", want, got)
		}
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
	if contains(got, "<p:Notes>") {
		t.Errorf("nil slice should be omitted, got: %s", got)
	}
	if contains(got, "<p:Empty>") {
		t.Errorf("empty slice should be omitted, got: %s", got)
	}
}

// TestMarshalEmbeddedInstance_OmitsZeroValues はゼロ値フィールドが出力されないことを検証する。
// CIM の SettingData では未指定 = デフォルト適用なので、ゼロ値を送ると意図しない上書きになる。
func TestMarshalEmbeddedInstance_OmitsZeroValues(t *testing.T) {
	settings := Msvm_VirtualHardDiskSettingData{
		Path:            `C:\a.vhdx`,
		MaxInternalSize: 1073741824,
		// VirtualDiskFormat / Type / BlockSize 等は未設定（ゼロ値）
	}

	got, err := marshalEmbeddedInstance(&settings, "Msvm_VirtualHardDiskSettingData", nsVirtV2+"/Msvm_VirtualHardDiskSettingData")
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}

	parsed, err := parseEmbeddedInstance(got)
	if err != nil {
		t.Fatalf("re-parse failed: %v", err)
	}

	// 設定したフィールドは含まれる
	if parsed["Path"] != `C:\a.vhdx` {
		t.Errorf("Path missing: %v", parsed)
	}
	// ゼロ値のフィールドは含まれない
	if _, ok := parsed["VirtualDiskFormat"]; ok {
		t.Errorf("VirtualDiskFormat should be omitted (zero value), got %q", parsed["VirtualDiskFormat"])
	}
	if _, ok := parsed["BlockSize"]; ok {
		t.Errorf("BlockSize should be omitted (zero value)")
	}
}
