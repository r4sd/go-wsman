package hyperv

import (
	"fmt"
	"sort"
	"strings"
)

// buildEndpointReference は WS-Addressing 形式の EndpointReference (EPR) XML を生成する。
//
// CIM Invoke のパラメータで参照型 (REF) を渡す際に使用する。例えば
// Msvm_VirtualSystemManagementService.DestroySystem の AffectedSystem は、
// 削除対象 VM (Msvm_ComputerSystem) への EPR を要求する。
//
// 出力例:
//
//	<a:EndpointReference xmlns:a="...">
//	  <a:Address>http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</a:Address>
//	  <a:ReferenceParameters>
//	    <w:ResourceURI xmlns:w="...">{resourceURI}</w:ResourceURI>
//	    <w:SelectorSet xmlns:w="...">
//	      <w:Selector Name="key">value</w:Selector>
//	    </w:SelectorSet>
//	  </a:ReferenceParameters>
//	</a:EndpointReference>
//
// セレクタの順序はテストの安定性のためキー名昇順に並べる。
func buildEndpointReference(resourceURI string, selectors map[string]string) string {
	const (
		nsAddressing = "http://schemas.xmlsoap.org/ws/2004/08/addressing"
		nsWsman      = "http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd"
		anonymous    = "http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous"
	)

	keys := make([]string, 0, len(selectors))
	for k := range selectors {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	fmt.Fprintf(&sb, `<a:EndpointReference xmlns:a=%q>`, nsAddressing)
	fmt.Fprintf(&sb, `<a:Address>%s</a:Address>`, anonymous)
	sb.WriteString(`<a:ReferenceParameters>`)
	fmt.Fprintf(&sb, `<w:ResourceURI xmlns:w=%q>%s</w:ResourceURI>`, nsWsman, xmlEscape(resourceURI))
	fmt.Fprintf(&sb, `<w:SelectorSet xmlns:w=%q>`, nsWsman)
	for _, k := range keys {
		fmt.Fprintf(&sb, `<w:Selector Name=%q>%s</w:Selector>`, k, xmlEscape(selectors[k]))
	}
	sb.WriteString(`</w:SelectorSet>`)
	sb.WriteString(`</a:ReferenceParameters>`)
	sb.WriteString(`</a:EndpointReference>`)
	return sb.String()
}
