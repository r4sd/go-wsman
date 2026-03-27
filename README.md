# go-wsman

Go による WS-Management (WS-Man) / CIM クライアントライブラリ。

## 概要

WS-Management プロトコルを Go でネイティブ実装し、PowerShell を介さずに Windows リモート管理（WinRM）を実現します。CIM (Common Information Model) 操作を直接 SOAP/XML メッセージとして構築・送信します。

## 主な機能

- SOAP エンベロープの構築・パース
- WS-Transfer 操作（Get, Put, Create, Delete）
- WS-Enumeration 操作（Enumerate, Pull）
- NTLM 認証による WinRM 接続
- SOAP Fault のパースとエラーハンドリング

## インストール

```bash
go get github.com/r4sd/go-wsman
```

## 使い方

```go
package main

import (
    "fmt"
    "log"

    "github.com/r4sd/go-wsman/wsman"
)

func main() {
    client, err := wsman.NewClient("https://host:5986/wsman",
        wsman.WithNTLM("username", "password"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // CIM インスタンスの取得
    resp, err := client.Get(
        "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_OperatingSystem",
    )
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp)
}
```

## 開発

```bash
# テスト実行
go test -race -v ./...

# ビルド
go build ./...

# Lint
golangci-lint run ./...
```

## ライセンス

MIT License
