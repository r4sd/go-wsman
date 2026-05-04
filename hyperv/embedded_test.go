package hyperv

import (
	"testing"
)

// TestParseEmbeddedInstance は CIM EmbeddedInstance XML 文字列を
// map[string]string に変換できることを検証する。
func TestParseEmbeddedInstance(t *testing.T) {
	t.Run("単純な要素のパース", func(t *testing.T) {
		xml := `<p:Msvm_VirtualHardDiskSettingData xmlns:p="http://schemas.microsoft.com/wbem/wsman/1/wmi/root/virtualization/v2/Msvm_VirtualHardDiskSettingData"><p:Path>C:\vm.vhdx</p:Path><p:MaxInternalSize>10737418240</p:MaxInternalSize><p:VirtualDiskFormat>3</p:VirtualDiskFormat></p:Msvm_VirtualHardDiskSettingData>`

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
		if got["VirtualDiskFormat"] != "3" {
			t.Errorf("VirtualDiskFormat: got %q", got["VirtualDiskFormat"])
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
	if parsed["VirtualDiskFormat"] != "3" {
		t.Errorf("VirtualDiskFormat round-trip: got %q", parsed["VirtualDiskFormat"])
	}
	if parsed["VirtualDiskType"] != "3" {
		t.Errorf("VirtualDiskType round-trip: got %q", parsed["VirtualDiskType"])
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
