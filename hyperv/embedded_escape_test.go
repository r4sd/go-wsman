package hyperv

import (
	"strings"
	"testing"
)

// TestCIMValueEscape_NumericCharRefs は embedded instance の値エスケープが
// **数値文字参照**を使うことを固定する (#173)。
//
// 🔴 Hyper-V の embedded instance パーサは `&amp;` / `&lt;` を受け付けず、
// 書き込みが失敗する。数値文字参照なら通る(2026-09-27 実機確認):
//
//	値        送り方   ReturnValue  読み戻し
//	"a & b"   &amp;    32768        (変化なし)
//	"a & b"   &#38;    0            "a & b"
//	"a < b"   &lt;     32773        (変化なし)
//	"a < b"   &#60;    0            "a < b"
//
// `&gt;` は実機でも受理されるが、`&` を数値参照にする以上揃える。
// `"` `'` は encoding/xml が元から数値参照で出すので**変更していない**。
func TestCIMValueEscape_NumericCharRefs(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"& は数値文字参照", "a & b", "a &#38; b"},
		{"< は数値文字参照", "a < b", "a &#60; b"},
		{"> は数値文字参照", "a > b", "a &#62; b"},
		{`" は数値文字参照`, `a " b`, "a &#34; b"},
		{"' は数値文字参照", "a ' b", "a &#39; b"},
		{"混在", `a & b < c > d " e ' f`, "a &#38; b &#60; c &#62; d &#34; e &#39; f"},
		{"改行は数値文字参照", "line1\nline2", "line1&#xA;line2"},
		{"CR は数値文字参照", "a\rb", "a&#xD;b"},
		{"TAB は数値文字参照", "a\tb", "a&#x9;b"},
		{"エスケープ不要な文字はそのまま", `C:\VMs\disk.vhdx`, `C:\VMs\disk.vhdx`},
		{"日本語はそのまま", "テスト メモ", "テスト メモ"},
		{"空文字", "", ""},
		// 入力に実体参照そのものが含まれても二重置換されないこと。
		{"入力が &amp; でも壊れない", "&amp;", "&#38;amp;"},
		// CDATA の中に入るので終端記号が生成されないこと。
		{"]]> は終端を作らない", "x]]>y", "x]]&#62;y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cimValueEscape(tt.in); got != tt.want {
				t.Errorf("cimValueEscape(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestCIMValueEscape_NoNamedEntities は名前付き実体参照を一切出さないことを固定する。
//
// `&amp;` / `&lt;` が出た瞬間に実機の書き込みが落ちるので、形を直接見張る。
// `&gt;` は実機では受理されるが、揃える方針なので同じく出さない。
func TestCIMValueEscape_NoNamedEntities(t *testing.T) {
	for _, in := range []string{"a & b", "a < b", "a > b", `a " b`, "a ' b", `&<>"'`, "&amp;&lt;"} {
		got := cimValueEscape(in)
		for _, bad := range []string{"&amp;", "&lt;", "&gt;", "&quot;", "&apos;"} {
			if strings.Contains(got, bad) {
				t.Errorf("cimValueEscape(%q) = %q に名前付き実体参照 %q が含まれる (#173)", in, got, bad)
			}
		}
	}
}

// TestCIMValueEscape_FiltersInvalidXMLChars は XML 不正文字が素通りしないことを固定する。
//
// C0 制御文字は XML 1.0 の Char 生成規則で CDATA 内でも禁止。値に NUL が混じると
// **SOAP エンベロープ全体が well-formed でなくなる**。encoding/xml はこれらを
// U+FFFD に置換するので、自前ループにせず stdlib に通してから参照だけ差し替える。
func TestCIMValueEscape_FiltersInvalidXMLChars(t *testing.T) {
	for _, in := range []string{"a\x00b", "a\x01b", "a\x0bb", "a\x0cb", "a\x1bb", "a\ufffeb", "a\uffffb"} {
		got := cimValueEscape(in)
		for _, r := range got {
			if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
				t.Errorf("cimValueEscape(%q) = %q に生の C0 制御文字 U+%04X が残っている", in, got, r)
			}
		}
		if strings.ContainsRune(got, '\ufffe') || strings.ContainsRune(got, '\uffff') {
			t.Errorf("cimValueEscape(%q) = %q に XML 不正文字が残っている", in, got)
		}
	}
}

// TestXMLEscape_EPRPathKeepsNamedEntities は EPR 経路のエスケープを変えていないことを固定する。
//
// EPR は CDATA ではなく SOAP 本文に生挿入され、**WinRM の標準 XML パーサ**が読む。
// Hyper-V の embedded instance パーサとは別物なので、名前付き実体参照のままでよい。
// ここを cimValueEscape に巻き込むと、#173 と無関係な経路の wire format が変わる。
func TestXMLEscape_EPRPathKeepsNamedEntities(t *testing.T) {
	if got, want := xmlEscape("a & b"), "a &amp; b"; got != want {
		t.Errorf("xmlEscape(%q) = %q, want %q (EPR は標準 XML パーサが読む)", "a & b", got, want)
	}
	if got, want := xmlEscape("a < b"), "a &lt; b"; got != want {
		t.Errorf("xmlEscape(%q) = %q, want %q", "a < b", got, want)
	}
}
