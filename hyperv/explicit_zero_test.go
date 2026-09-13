package hyperv

import (
	"strings"
	"testing"
)

// ptrTestStruct はポインタフィールドの意味論検証用。
type ptrTestStruct struct {
	InstanceID string `cim:"InstanceID"`
	Flag       *bool  `cim:"Flag"`
	Plain      bool   `cim:"Plain"`
}

// TestMarshalPointerField_Nil は nil ポインタが **PROPERTY ごと出ない** ことを検証する (#135)。
//
// ModifySystemSettings は「最小インスタンス」を要求する。触らないフィールドまで送ると
// read-only プロパティが乗ってジョブが Exception になるため、nil = 送らない は必須。
// 「常に送る」変異は値のアサーションだけでは捕まらないので、ここで明示的に押さえる。
func TestMarshalPointerField_Nil(t *testing.T) {
	got, err := marshalEmbeddedInstance(&ptrTestStruct{InstanceID: "x"}, "T", nsVirtV2)
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}
	if strings.Contains(got, `NAME="Flag"`) {
		t.Errorf("nil ポインタの PROPERTY が出力されている: %s", got)
	}
}

// TestMarshalPointerField_False は &false が **明示的に送られる** ことを検証する (#135)。
//
// これが本 Issue の本体。bool のゼロ値は false なので、値型のままでは
// marshalEmbeddedInstance のゼロ値スキップに掛かって一律で黙殺されていた。
func TestMarshalPointerField_False(t *testing.T) {
	f := false
	got, err := marshalEmbeddedInstance(&ptrTestStruct{InstanceID: "x", Flag: &f}, "T", nsVirtV2)
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}
	const want = `<PROPERTY NAME="Flag" TYPE="boolean"><VALUE>false</VALUE></PROPERTY>`
	if !strings.Contains(got, want) {
		t.Errorf("&false が送られていない\n want: %s\n got:  %s", want, got)
	}
}

// TestMarshalPointerField_True は非ゼロ値も従来どおり送られることを検証する。
func TestMarshalPointerField_True(t *testing.T) {
	tr := true
	got, err := marshalEmbeddedInstance(&ptrTestStruct{InstanceID: "x", Flag: &tr}, "T", nsVirtV2)
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}
	const want = `<PROPERTY NAME="Flag" TYPE="boolean"><VALUE>true</VALUE></PROPERTY>`
	if !strings.Contains(got, want) {
		t.Errorf("&true が送られていない\n want: %s\n got:  %s", want, got)
	}
}

// TestMarshalValueField_ZeroStillSkipped は値型のゼロ値スキップが変わっていないことを検証する。
// ポインタ化していないフィールドの意味論を変えると最小インスタンスの原則が崩れる。
func TestMarshalValueField_ZeroStillSkipped(t *testing.T) {
	got, err := marshalEmbeddedInstance(&ptrTestStruct{InstanceID: "x"}, "T", nsVirtV2)
	if err != nil {
		t.Fatalf("marshalEmbeddedInstance: %v", err)
	}
	if strings.Contains(got, `NAME="Plain"`) {
		t.Errorf("値型のゼロ値が送られている (最小インスタンスが崩れる): %s", got)
	}
}

// TestUnmarshalPointerField は read でポインタフィールドが埋まることを検証する。
func TestUnmarshalPointerField(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{{"TRUE", true}, {"FALSE", false}, {"false", false}}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			var got ptrTestStruct
			if err := UnmarshalList(map[string][]string{"Flag": {tc.raw}}, &got); err != nil {
				t.Fatalf("UnmarshalList: %v", err)
			}
			if got.Flag == nil {
				t.Fatalf("ポインタフィールドが nil のまま")
			}
			if *got.Flag != tc.want {
				t.Errorf("got %v, want %v", *got.Flag, tc.want)
			}
		})
	}
}

// TestUnmarshalPointerField_Absent はプロパティが応答に無ければ nil のままであることを検証する。
// 「未指定」と「false」を区別できることがポインタ化の目的なので、ここが崩れると意味が無い。
func TestUnmarshalPointerField_Absent(t *testing.T) {
	var got ptrTestStruct
	if err := UnmarshalList(map[string][]string{"InstanceID": {"x"}}, &got); err != nil {
		t.Fatalf("UnmarshalList: %v", err)
	}
	if got.Flag != nil {
		t.Errorf("応答に無いプロパティが nil でない: %v", *got.Flag)
	}
}
