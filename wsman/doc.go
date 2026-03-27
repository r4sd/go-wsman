// Package wsman は WS-Management (WS-Man) プロトコルの Go クライアント実装を提供する。
//
// WS-Man は DMTF (Distributed Management Task Force) が定義するリモート管理プロトコルで、
// SOAP/XML ベースのメッセージングにより CIM (Common Information Model) リソースを操作する。
//
// 主な機能:
//   - SOAP エンベロープの構築・パース
//   - WS-Transfer 操作 (Get, Put, Create, Delete)
//   - WS-Enumeration 操作 (Enumerate, Pull)
//   - NTLM 認証による WinRM 接続
//
// 基本的な使い方:
//
//	client, err := wsman.NewClient("https://host:5986/wsman",
//	    wsman.WithNTLM("username", "password"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// CIM インスタンスの取得
//	resp, err := client.Get(
//	    "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_OperatingSystem",
//	)
package wsman
