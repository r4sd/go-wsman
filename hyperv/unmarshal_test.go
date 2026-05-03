package hyperv

import (
	"testing"
)

// TestUnmarshal_BasicTypes は基本型（string, uint16）のマッピングを検証する。
func TestUnmarshal_BasicTypes(t *testing.T) {
	type target struct {
		Name         string `cim:"Name"`
		EnabledState uint16 `cim:"EnabledState"`
	}

	props := map[string]string{
		"Name":         "vm-guid-1",
		"EnabledState": "2",
	}

	var got target
	if err := Unmarshal(props, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if got.Name != "vm-guid-1" {
		t.Errorf("Name: got %q, want %q", got.Name, "vm-guid-1")
	}
	if got.EnabledState != 2 {
		t.Errorf("EnabledState: got %d, want %d", got.EnabledState, 2)
	}
}

// TestUnmarshal_NumericTypes は uint32, uint64, int 系の型変換を検証する。
func TestUnmarshal_NumericTypes(t *testing.T) {
	type target struct {
		U32 uint32 `cim:"U32"`
		U64 uint64 `cim:"U64"`
	}

	props := map[string]string{
		"U32": "4294967295",
		"U64": "18446744073709551615",
	}

	var got target
	if err := Unmarshal(props, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.U32 != 4294967295 {
		t.Errorf("U32: got %d", got.U32)
	}
	if got.U64 != 18446744073709551615 {
		t.Errorf("U64: got %d", got.U64)
	}
}

// TestUnmarshal_BoolType は bool 型変換（CIM の "TRUE"/"FALSE"）を検証する。
func TestUnmarshal_BoolType(t *testing.T) {
	type target struct {
		Enabled  bool `cim:"Enabled"`
		Disabled bool `cim:"Disabled"`
	}

	props := map[string]string{
		"Enabled":  "TRUE",
		"Disabled": "FALSE",
	}

	var got target
	if err := Unmarshal(props, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if !got.Enabled {
		t.Errorf("Enabled: got %v, want true", got.Enabled)
	}
	if got.Disabled {
		t.Errorf("Disabled: got %v, want false", got.Disabled)
	}
}

// TestUnmarshal_MissingProperty は props にないプロパティがゼロ値になることを検証する。
func TestUnmarshal_MissingProperty(t *testing.T) {
	type target struct {
		Name string `cim:"Name"`
		Foo  string `cim:"Foo"`
	}

	props := map[string]string{
		"Name": "vm-1",
	}

	var got target
	if err := Unmarshal(props, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.Name != "vm-1" {
		t.Errorf("Name: got %q", got.Name)
	}
	if got.Foo != "" {
		t.Errorf("Foo: got %q, want empty (zero value)", got.Foo)
	}
}

// TestUnmarshal_NoTag は cim タグなしフィールドがスキップされることを検証する。
func TestUnmarshal_NoTag(t *testing.T) {
	type target struct {
		Tagged   string `cim:"Tagged"`
		Untagged string
	}

	props := map[string]string{
		"Tagged":   "value1",
		"Untagged": "value2",
	}

	var got target
	if err := Unmarshal(props, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.Tagged != "value1" {
		t.Errorf("Tagged: got %q", got.Tagged)
	}
	if got.Untagged != "" {
		t.Errorf("Untagged: got %q, want empty (no tag, must be skipped)", got.Untagged)
	}
}

// TestUnmarshal_InvalidUint は uint パース失敗時に fail-fast でエラーを返すことを検証する。
func TestUnmarshal_InvalidUint(t *testing.T) {
	type target struct {
		EnabledState uint16 `cim:"EnabledState"`
	}

	props := map[string]string{
		"EnabledState": "abc",
	}

	var got target
	err := Unmarshal(props, &got)
	if err == nil {
		t.Fatal("expected error for invalid uint value, got nil")
	}
	// エラーメッセージにフィールド名とプロパティ名が含まれていること
	if !contains(err.Error(), "EnabledState") {
		t.Errorf("error message should contain field name: %v", err)
	}
}

// TestUnmarshal_NotPointer は非ポインタ引数でエラーを返すことを検証する。
func TestUnmarshal_NotPointer(t *testing.T) {
	type target struct {
		Name string `cim:"Name"`
	}
	var got target
	err := Unmarshal(map[string]string{"Name": "x"}, got) // ポインタじゃない
	if err == nil {
		t.Fatal("expected error for non-pointer arg, got nil")
	}
}

// TestUnmarshal_NilPointer は nil ポインタでエラーを返すことを検証する。
func TestUnmarshal_NilPointer(t *testing.T) {
	type target struct {
		Name string `cim:"Name"`
	}
	var p *target
	err := Unmarshal(map[string]string{"Name": "x"}, p)
	if err == nil {
		t.Fatal("expected error for nil pointer, got nil")
	}
}

// TestUnmarshal_VirtualHardDiskSettingData は VHD 設定の型変換を検証する。
func TestUnmarshal_VirtualHardDiskSettingData(t *testing.T) {
	props := map[string]string{
		"InstanceID":         "Microsoft:Definition\\1\\Default",
		"ElementName":        "vm-disk",
		"VirtualDiskFormat":  "3",
		"VirtualDiskType":    "3",
		"BlockSize":          "33554432",
		"LogicalSectorSize":  "512",
		"PhysicalSectorSize": "4096",
		"MaxInternalSize":    "10737418240",
		"Path":               `C:\VMs\vm.vhdx`,
		"ParentPath":         "",
	}

	var got Msvm_VirtualHardDiskSettingData
	if err := Unmarshal(props, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.InstanceID != "Microsoft:Definition\\1\\Default" {
		t.Errorf("InstanceID: got %q", got.InstanceID)
	}
	if got.ElementName != "vm-disk" {
		t.Errorf("ElementName: got %q", got.ElementName)
	}
	if got.VirtualDiskFormat != VHDFormatVHDX {
		t.Errorf("VirtualDiskFormat: got %d, want %d (VHDX)", got.VirtualDiskFormat, VHDFormatVHDX)
	}
	if got.VirtualDiskType != VHDTypeDynamic {
		t.Errorf("VirtualDiskType: got %d, want %d (Dynamic)", got.VirtualDiskType, VHDTypeDynamic)
	}
	if got.MaxInternalSize != 10737418240 {
		t.Errorf("MaxInternalSize: got %d", got.MaxInternalSize)
	}
	if got.Path != `C:\VMs\vm.vhdx` {
		t.Errorf("Path: got %q", got.Path)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
