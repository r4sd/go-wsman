package hyperv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r4sd/go-wsman/wsman"
)

// 管理サービス宛の Invoke に selector が付いていることを **横断で** 固定する (#177 項目 3)。
//
// 🔴 **selector を落とすと実機が落ちるが、症状はサービスごとに違う。**
//
//	VSMS  WBEM_E_INVALID_METHOD_PARAMETERS
//	VESMS WS-Man Fault [s:Receiver/w:InternalError]
//
// Hyper-V の WMI プロバイダ (WsmWmiPl.dll) はシングルトンサービスのメソッド実行時に
// インスタンスを特定する selector を要求する。VSMS では既知だったが VESMS は
// 見落とされていて #145 で初めて露見した。**残りのサービスも同じ地雷を踏みうる。**
//
// メソッドごとに golden 列を組んで振る舞いを見るのは高コストなので、
// **ソースを構文解析して「渡し忘れ」を直接禁じる**。
// 新しく Invoke を足して selector を忘れた瞬間に落ちる。
//
// 振る舞い側 (ヘッダに実際に載るか) は TestSwitchManagementServiceSelectors
// (VESMS) と各 selector ヘルパーの単体テストで押さえる。

// managementServiceSelectors は「この ResourceURI 定数を使う Invoke には、この selector
// ヘルパーを渡さなければならない」という対応表。
//
// 新しい管理サービスを足したらここにも足す。足し忘れると
// TestManagementServiceURIsAreCovered が落ちる。
var managementServiceSelectors = map[string]string{
	"msvmVirtualSystemManagementServiceURI":         "vsmsSelectors",
	"msvmImageManagementServiceURI":                 "imsSelectors",
	"msvmVirtualSystemSnapshotServiceURI":           "vssSelectors",
	"msvmVirtualEthernetSwitchManagementServiceURI": "vesmsSelectors",
}

// TestManagementServiceInvokesPassSelectors は管理サービス宛の Invoke / InvokeMulti すべてに
// 対応する selector ヘルパーが渡っていることを検証する。
func TestManagementServiceInvokesPassSelectors(t *testing.T) {
	calls := findManagementServiceInvokes(t)
	if len(calls) == 0 {
		t.Fatal("管理サービス宛の Invoke が 1 件も見つからない。検出が壊れている")
	}
	t.Logf("検出した Invoke: %d 件", len(calls))

	seen := make(map[string]int)
	for _, c := range calls {
		want := managementServiceSelectors[c.uriConst]
		seen[c.uriConst]++
		if !c.hasSelector(want) {
			t.Errorf("%s:%d %s(..., %s, %q, ...) に %s() が渡っていない。\n"+
				"  実機は selector 不足で失敗する (VSMS は WBEM_E_INVALID_METHOD_PARAMETERS、"+
				"VESMS は InternalError)",
				c.file, c.line, c.fn, c.uriConst, c.method, want)
		}
	}
	for uri, want := range managementServiceSelectors {
		if seen[uri] == 0 {
			t.Errorf("%s を使う Invoke が 1 件も見つからない (%s の検証が空振りしている)", uri, want)
		}
	}
}

// TestManagementServiceURIsAreCovered は管理サービスの URI 定数が対応表から漏れていないことを
// 検証する。新しい管理サービスを足して対応表に書き忘れると、Invoke の検証が空振りする。
func TestManagementServiceURIsAreCovered(t *testing.T) {
	fset := token.NewFileSet()
	for _, path := range nonTestGoFiles(t) {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("%s のパースに失敗: %v", path, err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					n := name.Name
					// 「管理サービス / スナップショットサービス」の URI 定数だけを対象にする。
					if !strings.HasSuffix(n, "ServiceURI") {
						continue
					}
					if _, ok := managementServiceSelectors[n]; !ok {
						t.Errorf("%s が managementServiceSelectors に無い。\n"+
							"  シングルトンサービスなら selector ヘルパーを作って対応表に足すこと "+
							"(付けないと実機が失敗する)", n)
					}
				}
			}
		}
	}
}

// TestSelectorHelpersReturnCreationClassName は各 selector ヘルパーが
// CreationClassName selector を 1 つだけ返すことを検証する。
//
// 実機が要求するのは「インスタンスを特定する selector」で、実測では
// CreationClassName 1 つで足りる (#145)。余計な selector を足すと
// 別の失敗 (SchemaValidationError 等) を招きうるので数も固定する。
func TestSelectorHelpersReturnCreationClassName(t *testing.T) {
	cases := []struct {
		helper string
		got    []wsman.Selector
		want   string
	}{
		{"vsmsSelectors", vsmsSelectors(), "Msvm_VirtualSystemManagementService"},
		{"imsSelectors", imsSelectors(), "Msvm_ImageManagementService"},
		{"vssSelectors", vssSelectors(), "Msvm_VirtualSystemSnapshotService"},
		{"vesmsSelectors", vesmsSelectors(), "Msvm_VirtualEthernetSwitchManagementService"},
	}
	for _, c := range cases {
		t.Run(c.helper, func(t *testing.T) {
			// 数も固定する。余計な selector を足すと別の失敗 (SchemaValidationError 等) を
			// 招きうる (EPR では <a:EndpointReference> ラッパーで実際に起きた)。
			if len(c.got) != 1 {
				t.Fatalf("selector を %d 個返した (want 1): %+v", len(c.got), c.got)
			}
			if c.got[0].Name != "CreationClassName" {
				t.Errorf("selector 名が %q (want CreationClassName)", c.got[0].Name)
			}
			if c.got[0].Value != c.want {
				t.Errorf("selector 値が %q (want %q)", c.got[0].Value, c.want)
			}
		})
	}
}

// --- ソース走査 ---

// invokeCall は検出した Invoke / InvokeMulti の呼び出し 1 件。
type invokeCall struct {
	file     string
	line     int
	fn       string // "Invoke" / "InvokeMulti"
	uriConst string // 第 2 引数の識別子名
	method   string // 第 3 引数の文字列リテラル
	args     []ast.Expr
}

// hasSelector は want で指定した selector ヘルパーの呼び出しが可変長引数に入っているかを返す。
func (c invokeCall) hasSelector(want string) bool {
	if want == "" {
		return false
	}
	for _, a := range c.args {
		call, ok := a.(*ast.CallExpr)
		if !ok {
			continue
		}
		id, ok := call.Fun.(*ast.Ident)
		if ok && id.Name == want {
			return true
		}
	}
	return false
}

// findManagementServiceInvokes は管理サービス宛の Invoke / InvokeMulti を全て集める。
//
// 第 2 引数が managementServiceSelectors のキー (URI 定数の識別子) である呼び出しだけを拾う。
// 変数経由で URI を渡す書き方は検出できないが、現状そのような呼び出しは無く、
// 将来書かれても TestManagementServiceInvokesPassSelectors の件数チェックで気付ける。
func findManagementServiceInvokes(t *testing.T) []invokeCall {
	t.Helper()
	fset := token.NewFileSet()
	var out []invokeCall
	for _, path := range nonTestGoFiles(t) {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("%s のパースに失敗: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			fn := sel.Sel.Name
			if fn != "Invoke" && fn != "InvokeMulti" {
				return true
			}
			if len(call.Args) < 3 {
				return true
			}
			uri, ok := call.Args[1].(*ast.Ident)
			if !ok {
				return true
			}
			if _, tracked := managementServiceSelectors[uri.Name]; !tracked {
				return true
			}
			method := ""
			if lit, ok := call.Args[2].(*ast.BasicLit); ok {
				method = strings.Trim(lit.Value, `"`)
			}
			out = append(out, invokeCall{
				file:     filepath.Base(path),
				line:     fset.Position(call.Pos()).Line,
				fn:       fn,
				uriConst: uri.Name,
				method:   method,
				args:     call.Args[3:],
			})
			return true
		})
	}
	return out
}

// nonTestGoFiles は hyperv パッケージの非テスト .go ファイル一覧を返す。
func nonTestGoFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ディレクトリを読めない: %v", err)
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		t.Fatal(".go ファイルが 1 件も見つからない")
	}
	return out
}
