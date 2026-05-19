# go-wsman プロジェクト規約

## プロジェクト概要

WS-Management (WS-Man) プロトコルの Go 実装。
最終目標: terraform-provider-hyperv の完全代替。

## アーキテクチャ

```
wsman/   ← WS-Man プロトコル層（SOAP, HTTP transport, NTLM/Cert 認証）
hyperv/  ← Hyper-V CIM バインディング層（Msvm_* クラスの型安全ラッパー）
```

- `hyperv/` は `wsman/` に依存する。逆方向の依存は禁止。
- 将来 terraform-provider は別リポジトリで `go-wsman` をインポートする。

## 設計書

- 全体設計: `docs/specs/2026-04-07-hyperv-cim-bindings-design.md`

## 命名規約

### CIM クラス・フィールド名

CIM 仕様の名前をそのまま使用する。Go の CamelCase 慣例より CIM との一貫性を優先。

```go
// Good: CIM 名そのまま
type Msvm_ComputerSystem struct {
    ElementName  string `cim:"ElementName"`
    EnabledState uint16 `cim:"EnabledState"`
}

// Bad: Go 風にリネーム
type VirtualMachine struct {
    Name  string `cim:"ElementName"`   // 元の CIM 名と乖離
    State uint16 `cim:"EnabledState"`
}
```

### ResourceURI 定数

```go
const (
    nsVirtV2 = "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/virtualization/v2"
    msvmComputerSystemURI = nsVirtV2 + "/Msvm_ComputerSystem"
)
```

## CIM 仕様確認 (新規 Msvm_* クラス実装前 **必須**)

CIM クラスを Go 構造体にバインドする際の一次資料は **Microsoft 公式 MOF**。URL は **アンダースコア除去 + 全小文字** のスラグ形式:

```
https://learn.microsoft.com/en-us/windows/win32/hyperv_v2/msvm-<class-slug>
例: Msvm_VirtualHardDiskSettingData → msvm-virtualharddisksettingdata
```

- フィールド名・型・配列性・列挙定数値を MOF と一致させる
- `cim:"..."` タグは **MOF プロパティ名と完全一致** 必須 (Go 識別子は別でも可)
- Issue 記述、terraform-provider-hyperv の Go コード、他言語ライブラリは **二次情報**、信用しない
- 新規クラス追加時は `hyperv/testdata/mof/{class_snake_case}.txt` に **struct が参照する CIM プロパティのみ** 保存し (網羅不要)、`cim_compliance_test.go` の reflect 突合テストで CI チェックさせる
- 既存クラスへの遡及は不要 (手動監査済)。気付いた時に fixture 追加で OK
- 詳細・経緯・失敗事例: Obsidian `40-Knowledge/cim/cim-bindings-mof-verification.md`

## テスト規約

### TDD サイクル（必須）

全ての機能実装は以下のサイクルで進める:

```
1. golden file 準備（実機ダンプ or 手書き XML）
2. Red:  テスト作成 → go test で失敗を確認
3. Green: 最小実装 → テスト通過
4. Refactor: 必要なら整理
5. go test -race -v ./... -count=1 で全テスト通過を確認
```

### golden file

- 配置: `{package}/testdata/`
- 命名: `{operation}_response_{class}.xml`（wsman パッケージの慣例に合わせる）
  - 例: `get_response_computersystem.xml`, `enumerate_response_computersystem.xml`
- ヘルパー: `loadGolden(t, filename)` を使用（wsman パッケージに実装済み）

### 統合テスト

- ビルドタグ: `//go:build integration`
- 環境変数: `WSMAN_ENDPOINT`, `WSMAN_USERNAME`, `WSMAN_PASSWORD`
- 実行: `go test -race -tags=integration -v ./{package}/...`
- Phase 1 は読み取り専用（VM の作成・削除は Phase 3 以降）

### テスト対象の判断

| 対象 | テスト |
|------|--------|
| Unmarshal（型変換ロジック） | 必須 |
| Client メソッド（golden file 検証） | 必須 |
| 統合テスト（実機接続） | Phase 完了時 |

## hyperv パッケージの実装パターン

### Unmarshal パターン

`wsman.Instance.Properties()` → `map[string]string` → `cim:` タグで struct にマッピング。

```go
resp, err := c.wsman.Get(ctx, resourceURI, selectors...)
if err != nil {
    return nil, err
}
var result SomeCIMClass
if err := Unmarshal(resp.Properties(), &result); err != nil {
    return nil, err
}
return &result, nil
```

### エラーハンドリング

- `*wsman.Fault`: SOAP Fault（AccessDenied 等）→ そのまま返す
- Unmarshal エラー: `fmt.Errorf("failed to unmarshal %s: %w", className, err)` でラップ
- 通信エラー: wsman 層がハンドリング済み

### 定数定義

CIM 列挙値は `uint16` 定数として `types.go` に定義する。

```go
const (
    EnabledStateEnabled  uint16 = 2     // Running
    EnabledStateDisabled uint16 = 3     // Off
)
```

## 検証コマンド

```bash
# 全テスト
go test -race -v ./... -count=1

# 特定パッケージ
go test -race -v ./hyperv/... -count=1

# ベンチマーク
go test -bench=. -benchmem ./wsman/...

# vet + build
go vet ./... && go build ./...

# 統合テスト（実機接続時のみ）
WSMAN_ENDPOINT=https://host:5986/wsman \
WSMAN_USERNAME=user \
WSMAN_PASSWORD=pass \
go test -race -tags=integration -v ./hyperv/...
```
