package hyperv

import (
	"strings"
	"testing"
)

// TestParseCimTag は cim タグの「名前 + オプション」分解を検証する。
func TestParseCimTag(t *testing.T) {
	cases := []struct {
		tag      string
		wantName string
		wantDT   bool
	}{
		{"ElementName", "ElementName", false},
		{"AutomaticCriticalErrorActionTimeout,datetime", "AutomaticCriticalErrorActionTimeout", true},
		{"", "", false},
		{"Foo,", "Foo", false},
		{"Foo,unknown", "Foo", false},
	}
	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			name, isDT := parseCimTag(tc.tag)
			if name != tc.wantName || isDT != tc.wantDT {
				t.Errorf("parseCimTag(%q) = (%q, %v), want (%q, %v)", tc.tag, name, isDT, tc.wantName, tc.wantDT)
			}
		})
	}
}

// TestISO8601ToCIMInterval は書き込み用の CIM ネイティブ interval への変換を検証する。
//
// 実機で確定した非対称 (2026-09-13、#119):
//
//	read  : ISO 8601 duration     "P0DT0H45M0S"
//	write : CIM ネイティブ interval "00000000004500.000000:000"
//
// ISO のまま送ると TYPE="datetime" でも ErrorCode=32768 になる。
func TestISO8601ToCIMInterval(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"P0DT0H45M0S", "00000000004500.000000:000", false},
		{"P0DT0H0M0S", "00000000000000.000000:000", false},
		{"P0DT0H30M0S", "00000000003000.000000:000", false},
		{"P0DT1H2M3S", "00000000010203.000000:000", false},
		{"P1DT0H0M0S", "00000001000000.000000:000", false},
		{"P12345678DT23H59M59S", "12345678235959.000000:000", false},
		// 既に CIM ネイティブなら素通し (read 由来でない値を代入された場合)
		{"00000000004500.000000:000", "00000000004500.000000:000", false},
		{"", "", true},
		{"45", "", true},
		{"PT45M", "", true},
		{"P123456789DT0H0M0S", "", true}, // 日が 8 桁に収まらない
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := iso8601ToCIMInterval(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("iso8601ToCIMInterval(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("iso8601ToCIMInterval(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestMarshalEmbeddedInstance_Datetime は datetime タグ付きフィールドが
// TYPE="datetime" かつ CIM ネイティブ書式で出ることを検証する (#119)。
//
// PROPERTY 要素を丸ごと比較する。Contains で "datetime" と値を別々に見ると、
// 型と値の対応が崩れていても通ってしまう。
func TestMarshalEmbeddedInstance_Datetime(t *testing.T) {
	sd := &Msvm_VirtualSystemSettingData{
		InstanceID:                          "Microsoft:GUID",
		AutomaticCriticalErrorActionTimeout: "P0DT0H45M0S",
		AutomaticStartupActionDelay:         "P0DT0H1M30S",
	}
	got, err := marshalEmbeddedInstance(sd, "Msvm_VirtualSystemSettingData", nsVirtV2)
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}

	for _, want := range []string{
		`<PROPERTY NAME="AutomaticCriticalErrorActionTimeout" TYPE="datetime"><VALUE>00000000004500.000000:000</VALUE></PROPERTY>`,
		`<PROPERTY NAME="AutomaticStartupActionDelay" TYPE="datetime"><VALUE>00000000000130.000000:000</VALUE></PROPERTY>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("PROPERTY 要素が一致しない\n want: %s\n got:  %s", want, got)
		}
	}
	// タグ名にオプションが混ざっていないこと (NAME はプロパティ名のみ)。
	if strings.Contains(got, ",datetime") {
		t.Errorf("NAME 属性にタグオプションが漏れている: %s", got)
	}
	// datetime 指定の無い string フィールドは従来どおり TYPE="string"。
	if !strings.Contains(got, `<PROPERTY NAME="InstanceID" TYPE="string"><VALUE>Microsoft:GUID</VALUE></PROPERTY>`) {
		t.Errorf("datetime 指定の無いフィールドの型が変わっている: %s", got)
	}
}

// TestMarshalEmbeddedInstance_DatetimeInvalid は変換できない値で fail-loud することを検証する。
//
// 黙って送ると ErrorCode=32768 になるだけで、呼び出し側からは
// 「成功したのに変わらない」に見える (本リポジトリで繰り返している事故の型)。
func TestMarshalEmbeddedInstance_DatetimeInvalid(t *testing.T) {
	sd := &Msvm_VirtualSystemSettingData{
		InstanceID:                          "Microsoft:GUID",
		AutomaticCriticalErrorActionTimeout: "45",
	}
	if _, err := marshalEmbeddedInstance(sd, "Msvm_VirtualSystemSettingData", nsVirtV2); err == nil {
		t.Fatal("変換できない datetime 値がエラーにならない")
	}
}
