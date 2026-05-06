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

### Git hooks のセットアップ（初回 clone 時）

`gofmt` 違反などを CI 待ちせずローカルで検出するため、pre-commit フックを用意しています。
clone 後に 1 回だけ以下を実行してください。

```bash
./scripts/install-hooks.sh
```

`core.hooksPath` が `.githooks/` に設定され、コミット時に `.githooks/pre-commit` が自動実行されます。
緊急回避は `git commit --no-verify`（CI で結局落ちるので例外的な場合のみ）。

## ライセンス

MIT License
