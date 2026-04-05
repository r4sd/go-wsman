package wsman

// WS-Management プロトコルで使用する XML 名前空間
const (
	// NSEnvelope は SOAP 1.2 エンベロープ名前空間
	NSEnvelope = "http://www.w3.org/2003/05/soap-envelope"

	// NSAddressing は WS-Addressing 名前空間
	NSAddressing = "http://schemas.xmlsoap.org/ws/2004/08/addressing"

	// NSWsman は WS-Management (DMTF) 名前空間
	NSWsman = "http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd"

	// NSWsmanMicrosoft は WS-Management (Microsoft 拡張) 名前空間
	NSWsmanMicrosoft = "http://schemas.microsoft.com/wbem/wsman/1/wsman.xsd"

	// NSTransfer は WS-Transfer 名前空間
	NSTransfer = "http://schemas.xmlsoap.org/ws/2004/09/transfer"

	// NSEnumeration は WS-Enumeration 名前空間
	NSEnumeration = "http://schemas.xmlsoap.org/ws/2004/09/enumeration"

	// NSWsmanFault は WS-Management Fault 名前空間
	NSWsmanFault = "http://schemas.microsoft.com/wbem/wsman/1/wsmanfault"

	// NSXMLSchema は XML Schema Instance 名前空間
	NSXMLSchema = "http://www.w3.org/2001/XMLSchema-instance"

	// NSCIMBinding は CIM Binding 名前空間
	NSCIMBinding = "http://schemas.dmtf.org/wbem/wsman/1/cimbinding.xsd"

	// DialectWQL は WQL (WMI Query Language) フィルタの Dialect URI
	DialectWQL = "http://schemas.microsoft.com/wbem/wsman/1/WQL"
)

// WS-Transfer アクション URI
const (
	ActionGet    = NSTransfer + "/Get"
	ActionPut    = NSTransfer + "/Put"
	ActionCreate = NSTransfer + "/Create"
	ActionDelete = NSTransfer + "/Delete"

	ActionGetResponse    = NSTransfer + "/GetResponse"
	ActionPutResponse    = NSTransfer + "/PutResponse"
	ActionCreateResponse = NSTransfer + "/CreateResponse"
	ActionDeleteResponse = NSTransfer + "/DeleteResponse"
)

// WS-Enumeration アクション URI
const (
	ActionEnumerate         = NSEnumeration + "/Enumerate"
	ActionEnumerateResponse = NSEnumeration + "/EnumerateResponse"
	ActionPull              = NSEnumeration + "/Pull"
	ActionPullResponse      = NSEnumeration + "/PullResponse"
)

// WS-Addressing 定数
const (
	AddressAnonymous = NSAddressing + "/role/anonymous"
)
