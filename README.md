# go-wsman

Go による WS-Management (WS-Man) / CIM クライアントライブラリ。**Hyper-V を PowerShell なしで操作する。**

## 概要

Hyper-V の実体は WMI/CIM クラス群 (`root/virtualization/v2` の `Msvm_*`) で、
PowerShell の Hyper-V モジュールはその薄いラッパーにすぎない。

このライブラリは WS-Management プロトコル(WinRM が話す SOAP/XML)を Go でネイティブに実装し、
**PowerShell を一切起動せずに** Hyper-V を操作する。

そのため、

- ホスト側に PowerShell 実行環境が要らない(バージョン差の影響を受けない)
- エラーを文字列ではなく型で受け取れる(`*wsman.Fault` / transport エラー / unmarshal エラー)
- CIM の列挙値を `uint16` のまま扱える(往復で型が落ちない)
- ホスト OS の表示言語に依存しない

なぜ PowerShell を避けたか、既存の provider と何が違うかは
[ADR-0001](docs/adr/0001-powershell-free.md) に記録している。

## 構成

```
wsman/   ← WS-Man プロトコル層（SOAP, HTTP transport, NTLM / 証明書認証）
hyperv/  ← Hyper-V CIM バインディング層（Msvm_* の型安全ラッパー）
```

`hyperv/` は `wsman/` に依存する。逆方向の依存はない。
`wsman/` だけを使えば Hyper-V 以外の CIM 名前空間(`root/cimv2` 等)も操作できる。

## 対応している操作

### `wsman` 層

- SOAP エンベロープの構築・パース
- WS-Transfer: Get / Put / Create / Delete
- WS-Enumeration: Enumerate / Pull
- メソッド呼び出し (Invoke)
- SOAP Fault のパースとエラーハンドリング
- NTLM 認証、TLS クライアント証明書認証
- 並行実行に耐える接続プール([ADR-0004](docs/adr/0004-authentication.md))

### `hyperv` 層

| 領域 | 主な操作 |
|---|---|
| VM ライフサイクル | `DefineSystem`, `UpdateVm`, `DestroySystem`, `GetComputerSystem`, `ListComputerSystems` |
| 電源状態 | `StartVM`, `ShutdownVM`, `TurnOffVM`, `PauseVM`, `ResumeVM`, `SaveVM`, `RequestStateChange` |
| メモリ / CPU | `GetMemorySettings`, `SetMemorySettings`, `GetProcessorSettings`, `SetProcessorSettings` |
| ストレージ | `CreateVirtualHardDisk`, `ResizeVirtualHardDisk`, `AttachVHD`, `AttachDVD`, `DetachStorage`, `AddScsiController` |
| ネットワーク | `AddNetworkAdapter`, `RemoveNetworkAdapter`, `AddNetworkAdapterVlan`, `ListNetworkAdapters` |
| 仮想スイッチ | `CreateSwitch`, `DestroySwitch`, `ListVirtualEthernetSwitches` |
| ファームウェア | `ListBootSources`, `BootSourceRef`(Gen2 のブート順・セキュアブート) |
| チェックポイント | `CreateVmCheckpoint`, `ApplyVmCheckpoint`, `RenameVmCheckpoint`, `DestroyVmCheckpoint`, `ListVmCheckpoints` |
| 統合サービス | `GetIntegrationServiceEnabled`, `SetIntegrationServiceEnabled`, `ListIntegrationServices` |
| GPU / ゲスト情報 | `ListGpuAdapters`, `ListGuestNetworkAdapterConfigurations` |
| 非同期ジョブ | `WaitForJob`, `WaitForJobEPR`(CIM のメソッドは非同期で返ることがある) |

### 対応していないもの

- **Kerberos 認証**(Active Directory ドメイン環境)— Issue #8 で追跡
- Hyper-V レプリカ、フェールオーバークラスタリング(Windows Server 専用機能)
- Windows Server Backup(CIM の管理外)

## 前提条件

- 対象ホストで **WinRM が HTTPS (既定 5986) で待ち受けていること**
- ワークグループ構成なら NTLM 認証、証明書を配布できるなら TLS クライアント証明書
- 動作確認は Windows 11 Hyper-V (`root/virtualization/v2`) で行っている

## インストール

```bash
go get github.com/r4sd/go-wsman
```

## 使い方

### Hyper-V を操作する

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/r4sd/go-wsman/hyperv"
    "github.com/r4sd/go-wsman/wsman"
)

func main() {
    client, err := hyperv.NewClient("https://<hyperv-host>:5986/wsman",
        wsman.WithNTLM("<user>", "<password>"),
    )
    if err != nil {
        log.Fatal(err)
    }

    vms, err := client.ListComputerSystems(context.Background())
    if err != nil {
        log.Fatal(err)
    }
    for _, vm := range vms {
        fmt.Printf("%s  state=%d\n", vm.ElementName, vm.EnabledState)
    }
}
```

### CIM を直接叩く

```go
client, err := wsman.NewClient("https://<hyperv-host>:5986/wsman",
    wsman.WithNTLM("<user>", "<password>"),
)
if err != nil {
    log.Fatal(err)
}

// Get(ctx, resourceURI, selectors...) — selector を渡すとインスタンスを絞り込める
resp, err := client.Get(context.Background(),
    "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_OperatingSystem",
)
if err != nil {
    log.Fatal(err)
}
fmt.Println(resp.Properties())
```

### エラーの扱い

失敗した層が型で分かる。

```go
vm, err := client.GetComputerSystem(ctx, guid)
if err != nil {
    var fault *wsman.Fault
    if errors.As(err, &fault) {
        // Hyper-V 側が拒否した (AccessDenied 等)。fault.Code / fault.Subcode / fault.Reason
    }
    // それ以外は通信エラーか、CIM プロパティの型不一致
}
```

## 開発

```bash
go test -race -v ./...      # テスト
go build ./...              # ビルド
golangci-lint run ./...     # Lint
```

### 統合テスト(実機接続)

通常の `go test ./...` では動かない(`//go:build integration` で分離してある)。

```bash
WSMAN_ENDPOINT=https://<hyperv-host>:5986/wsman \
WSMAN_USERNAME=<user> \
WSMAN_PASSWORD=<password> \
go test -race -tags=integration -v ./hyperv/...
```

**書き込みを伴うテストは既定で skip される。** 実行するには追加のゲートが要る。

| 環境変数 | 役割 |
|---|---|
| `HYPERV_TEST_ALLOW_MUTATION` | 実機への書き込みを許可する |
| `HYPERV_TEST_TARGET_VM_NAME` | 対象 VM の表示名 |
| `WSMAN_INSECURE` | `true` にすると TLS 証明書の検証をスキップする(自己署名証明書のホスト向け) |

テストフィクスチャの方針(golden file は実機ダンプのみ、手書き禁止)は
[ADR-0003](docs/adr/0003-test-fixtures.md) を参照。

### Git hooks のセットアップ（初回 clone 時）

`gofmt` 違反などを CI 待ちせずローカルで検出するため、pre-commit フックを用意している。
clone 後に 1 回だけ実行する。

```bash
./scripts/install-hooks.sh
```

`core.hooksPath` が `.githooks/` に設定され、コミット時に `.githooks/pre-commit` が自動実行される。
緊急回避は `git commit --no-verify`(CI で結局落ちるので例外的な場合のみ)。

## 設計の記録

| ADR | 内容 |
|---|---|
| [0001](docs/adr/0001-powershell-free.md) | PowerShell に依存しない理由 |
| [0002](docs/adr/0002-cim-binding.md) | CIM クラスのバインディング方式 |
| [0003](docs/adr/0003-test-fixtures.md) | テストフィクスチャの作り方 |
| [0004](docs/adr/0004-authentication.md) | 認証とコネクション管理 |

## ライセンス

MIT License
