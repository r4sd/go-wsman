package hyperv

import (
	"fmt"
	"sort"
	"strings"
)

// wmiObjectPath は embedded instance の string プロパティ (Msvm_*SettingData.Parent や
// HostResource 等) 用の WMI オブジェクトパス文字列を生成する。
//
// これらのプロパティは CIM 上 string 型で、値は WMI オブジェクトパス
// (\\HOST\namespace:Class.Key="value") を要求する。REF パラメータ (AffectedConfiguration
// 等) の WS-Addressing EPR (buildEndpointReference) とは形式が別。両者を混同すると
// AddResourceSettings が「リソースを追加できませんでした」(ErrorCode=32773) で失敗する。
//
// host が空なら \\HOST\ 前置を省いた相対パス。値内の \ は WMI パス引用規則で \\ に、" は \" に
// エスケープする (前置部 \\HOST\namespace はエスケープ対象外)。キーは名前昇順で安定化。
func wmiObjectPath(host, resourceURI string, keys map[string]string) string {
	// resourceURI ("http://.../wmi/root/virtualization/v2/Msvm_X") から
	// namespace ("root/virtualization/v2") と class ("Msvm_X") を取り出す。
	tail := resourceURI
	if i := strings.Index(tail, "/wmi/"); i >= 0 {
		tail = tail[i+len("/wmi/"):]
	}
	ns, class := tail, ""
	if j := strings.LastIndex(tail, "/"); j >= 0 {
		ns, class = tail[:j], tail[j+1:]
	}

	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)

	var sb strings.Builder
	if host != "" {
		fmt.Fprintf(&sb, `\\%s\`, host)
	}
	fmt.Fprintf(&sb, "%s:%s", ns, class)
	for i, k := range names {
		if i == 0 {
			sb.WriteByte('.')
		} else {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `%s="%s"`, k, wmiPathValueEscape(keys[k]))
	}
	return sb.String()
}

// wmiPathValueEscape は WMI オブジェクトパスのキー値 (引用符内) をエスケープする。
func wmiPathValueEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// buildEndpointReference は WS-Addressing 形式の EndpointReference (EPR) XML を生成する。
//
// CIM Invoke のパラメータで参照型 (REF) を渡す際に使用する。例えば
// Msvm_VirtualSystemManagementService.DestroySystem の AffectedSystem は、
// 削除対象 VM (Msvm_ComputerSystem) への EPR を要求する。
//
// MS WS-Man の REF パラメータは EndpointReferenceType であり、パラメータ要素 (例:
// <p:AffectedSystem>) の直下に <a:Address> と <a:ReferenceParameters> を置く。
// <a:EndpointReference> ラッパーで包むと SchemaValidationError になる (実機 acc test で確認)。
// a: (WS-Addressing) / w: (wsman.xsd) prefix は SOAP エンベロープ root で宣言済みのため継承する。
//
// 出力例 (param 要素内に挿入される):
//
//	<a:Address>http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</a:Address>
//	<a:ReferenceParameters>
//	  <w:ResourceURI xmlns:w="...">{resourceURI}</w:ResourceURI>
//	  <w:SelectorSet xmlns:w="..."><w:Selector Name="key">value</w:Selector></w:SelectorSet>
//	</a:ReferenceParameters>
//
// セレクタの順序はテストの安定性のためキー名昇順に並べる。
func buildEndpointReference(resourceURI string, selectors map[string]string) string {
	const (
		nsWsman   = "http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd"
		anonymous = "http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous"
	)

	keys := make([]string, 0, len(selectors))
	for k := range selectors {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	fmt.Fprintf(&sb, `<a:Address>%s</a:Address>`, anonymous)
	sb.WriteString(`<a:ReferenceParameters>`)
	fmt.Fprintf(&sb, `<w:ResourceURI xmlns:w=%q>%s</w:ResourceURI>`, nsWsman, xmlEscape(resourceURI))
	fmt.Fprintf(&sb, `<w:SelectorSet xmlns:w=%q>`, nsWsman)
	for _, k := range keys {
		fmt.Fprintf(&sb, `<w:Selector Name=%q>%s</w:Selector>`, k, xmlEscape(selectors[k]))
	}
	sb.WriteString(`</w:SelectorSet>`)
	sb.WriteString(`</a:ReferenceParameters>`)
	return sb.String()
}
