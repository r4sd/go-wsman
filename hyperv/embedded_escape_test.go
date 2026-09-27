package hyperv

import "testing"

// TestXMLEscape_NumericCharRefs は embedded instance の値エスケープが
// **数値文字参照**を使うことを固定する (#173)。
//
// 🔴 Hyper-V の embedded instance パーサは名前付き実体参照 `&amp;` / `&lt;` を
// 受け付けず、書き込みが失敗する。数値文字参照なら通る(2026-09-27 実機確認):
//
//	値        送り方      ReturnValue  読み戻し
//	"a & b"   &amp;       32768        (変化なし)
//	"a & b"   &#38;       0            "a & b"
//	"a < b"   &lt;        32773        (変化なし)
//	"a < b"   &#60;       0            "a < b"
//
// Notes 固有ではない。ElementName (スカラー) でも同じで、
// marshalEmbeddedInstance を通る全ての文字列プロパティが対象。
//
// `>` `"` `'` は名前付きでも通るが、`&` を数値参照にする以上まとめて揃える。
func TestXMLEscape_NumericCharRefs(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"& は数値文字参照", "a & b", "a &#38; b"},
		{"< は数値文字参照", "a < b", "a &#60; b"},
		{"> は数値文字参照", "a > b", "a &#62; b"},
		{"混在", "a & b < c > d", "a &#38; b &#60; c &#62; d"},
		{"エスケープ不要な文字はそのまま", `C:\VMs\disk.vhdx`, `C:\VMs\disk.vhdx`},
		{"日本語はそのまま", "テスト メモ", "テスト メモ"},
		// 改行は従来から数値文字参照 (&#xA;) になっており、実機はこれを受理する
		// (改行入り Notes の round-trip を 2026-09-27 に確認)。変える必要はない。
		{"改行は数値文字参照", "line1\nline2", "line1&#xA;line2"},
		{"空文字", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := xmlEscape(tt.in); got != tt.want {
				t.Errorf("xmlEscape(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestXMLEscape_NoNamedEntities は名前付き実体参照を一切出さないことを固定する。
// これが出た瞬間に実機の書き込みが落ちるので、形を直接見張る。
func TestXMLEscape_NoNamedEntities(t *testing.T) {
	for _, in := range []string{"a & b", "a < b", "a > b", `a " b`, "a ' b", "&<>\"'"} {
		got := xmlEscape(in)
		for _, bad := range []string{"&amp;", "&lt;", "&gt;", "&quot;", "&apos;"} {
			if contains(got, bad) {
				t.Errorf("xmlEscape(%q) = %q に名前付き実体参照 %q が含まれる。"+
					"実機はこれを受け付けない (#173)", in, got, bad)
			}
		}
	}
}
