# go-wsman プロジェクト規約

## このファイルの位置づけ

**ここは「今どう開発するか」だけを書く。** 経緯や却下した案は書かない(古い記述が残ると、どれが現行か分からなくなるため)。

| 知りたいこと | 見る場所 |
|---|---|
| **使い方** | [`README.md`](README.md) |
| **今どう開発するか** | このファイル(常に最新だけ。古い記述は上書きする) |
| **なぜそう作ったか / なぜやらないのか** | [`docs/adr/`](docs/adr/README.md)(追記のみ。過去の判断を消さない) |
| **実装前の設計** | [`docs/specs/`](docs/specs/) |

> ⚠️ **公開リポジトリ。** 実環境のアドレス・ホスト名・アカウント名を書かない。
> 例示は RFC 5737 の文書用アドレスかプレースホルダを使う(CI が混入を落とす)。

---

## プロジェクト概要

WS-Management (WS-Man) プロトコルの Go 実装。
最終目標: terraform-provider-hyperv の完全代替。

> **shadow 移行の DoD**: provider の PS→go-wsman 移行を 1 機能=1 縦スライスで進める際の「完了の
> 定義」は terraform-provider-hyperv リポジトリの `CLAUDE.md`(シャドウ移行 DoD)を参照。
> go-wsman 側 primitive を足す時は、その ①MOF 一次資料検証 → ②TDD → provider 側 ③shadow の
> 流れの一部を担う。
>
> **provider を伴わない go-wsman 単独の PR も DoD ⑥(Fable 批判的レビュー)の対象**。PR 作成と
> 同時にバックグラウンドで起動し CI と並走させ、**CI green だけでマージしない**。2026-08-01 に
> 単独 PR でこれを飛ばし、事後レビューで CONFIRMED(手書き golden が実機に無い挙動を仕様として
> 固定していた)が出た。golden 手書きは本リポジトリ特有の事故源なので、単独 PR ほど効く。

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

CIM クラスを Go 構造体にバインドする際の一次資料は **Microsoft 公式 MOF**。URL は **アンダースコアをハイフンに置換 + 全小文字** のスラグ形式:

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
1. golden file 準備（**実機ダンプを匿名化して使う**。手書きは最後の手段)
2. Red:  テスト作成 → go test で失敗を確認
3. Green: 最小実装 → テスト通過
4. Refactor: 必要なら整理
5. go test -race -v ./... -count=1 で全テスト通過を確認
```

### fixture は録音する (手書きは関所で弾く)

> 🔴 **fixture を手で書かない。録音する。**
>
> 手書き golden が「実機に無い挙動」を仕様として固定する事故を **7 回**繰り返している
> (#124 / #126 / #136 / #137 / #143 / provider #145 / PR #152)。
> いずれも単体テストは緑のまま、実機で初めて落ちた。
>
> **警告文は 1 度も効かなかった。** 5 回目は「4 回繰り返している」とこのファイルに
> 書いた同じセッションが数時間後に起こしている。知識ではなく強制力の問題。
> 一方 #138 で `Unmarshal` を削除して**選択肢自体を消した**型は再発していない。
>
> だから「実機から採取したと書いてあるか」は見ない (主張の真偽は機械検証できない)。
> 録音器が書いた印と本文の sha256 を持つファイルだけを実機由来として扱う (`internal/guard`)。
>
> ⚠️ **これは証明ではない。** 印も sha256 も自分で計算して貼れる。止まるのは
> 「それらしい XML を思いつきで書く」経路と「録音した後で値を調整する」経路で、
> 過去 7 件はすべてこの 2 つ。**摩擦を上げる仕組み**と理解しておく。

**実機の応答が要るとき**: 録音する。

```bash
WSMAN_RECORD_DIR=./recorded go test -tags=integration ./hyperv/... -run TestIntegration_Xxx
```

統合テストを回すだけで、応答が 1 つずつ匿名化済みの XML として貯まる。
使うものを `testdata/` へコピーするだけでよい。

匿名化するもの: GUID / 接続先ホスト / プライベート IP / VM 表示名・コンピュータ名。
最後のものはパターンで拾えないので**実機から自動収集する** (環境変数で人が渡す形にすると
設定し忘れで静かに漏れる)。XML エスケープ後の形と大文字小文字の違いも含めて伏せ、
保存後に読み返して検証し、残っていたら落ちる。

**合成データが要るとき**: `testdata/synthetic/` に置き、ファイル内に
`derived-from: <録音物のパス>` を書く。派生元の存在は CI が確かめる。

**ソースに XML を直接書かない。** testdata に関所があってもそこで迂回できるので、
既存ファイルは出現数まで固定してある (追記も検出される)。

- 配置: `{package}/testdata/`
- 命名: `{operation}_response_{class}.xml`（wsman パッケージの慣例に合わせる）
  - 例: `get_response_computersystem.xml`, `enumerate_response_computersystem.xml`
- ヘルパー: `loadGolden(t, filename)` を使用（wsman パッケージに実装済み）

### 書き込み可否は MOF から判断しない

MOF の `Access type` は `ModifySystemSettings` の受理を**予測しない**。実機で確認する。

| フィールド | MOF | 実機 |
|---|---|---|
| `SecureBootTemplateId` | Read-only(「ModifyVirtualSystem で変更可」の但し書きあり) | ✅ 書ける |
| `AutomaticCriticalErrorActionTimeout` | **Read/write** | ✅ 書ける(ただし送り方が特殊。下記) |
| `AutomaticStartupActionDelay` | Read-only | ✅ 書ける(同上) |

MOF に「ModifyVirtualSystem で変更できる」と明記があれば書ける可能性が高い。
無ければ**実機で確かめるまで書けると仮定しない**。

逆に `ErrorCode=32768` を「書けない」と早合点しない。上の interval 2 件は長く
「書けない」と扱われていたが、実際は**こちらの送り方が間違っていた**
(TYPE 属性が `string`、値が ISO 8601 のまま。#119)。

配列性も同様で、`Notes` は MOF が `string[]` だが**実質単一値**
(複数要素を送ると先頭以外が捨てられる)。

### read と write で wire format が違うことがある

同じプロパティでも読み書きで形式が変わる。read の値をそのまま送り返すと落ちる。

| プロパティ | read | write |
|---|---|---|
| `AutomaticStartupActionDelay` / `AutomaticCriticalErrorActionTimeout` | ISO 8601 duration `P0DT0H30M0S` | CIM ネイティブ `00000000003000.000000:000` + `TYPE="datetime"` |
| `BootSourceOrder` | 参照文字列(read 形式) | 別の参照文字列形式 |

datetime 型は `cim:"<name>,datetime"` タグで指定する。marshal 側が型と値の
両方を変換する。**片方だけでは実機が `ErrorCode=32768` を返す**(2×2 を全数試行して確認)。

### ミューテーション検証の落とし穴

「N/N 撃墜」を報告する前に確認する。

| 罠 | 対処 |
|---|---|
| コンパイルエラーで落ちただけ | 変異は**ビルドが通る形**にする (`if false` ではなく `_ = x` を添える) |
| 純関数だけ変異させた | 呼び出し経路を通すテスト (`httptest` 等) を足す |
| 応答列の余りを検査していない | テスト終了時に全応答を消費したか検証する |
| `Contains` で引数を検証 | **どのプロパティにどちらが入っているか**を見る |

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

`PropertiesList()` → `map[string][]string` → `cim:` タグで struct にマッピング。

```go
resp, err := c.wsman.Get(ctx, resourceURI, selectors...)
if err != nil {
    return nil, err
}
var result SomeCIMClass
if err := UnmarshalList(resp.PropertiesList(), &result); err != nil {
    return nil, err
}
return &result, nil
```

> ⚠️ **`UnmarshalList` / `PropertiesList()` だけを使う。** スカラー専用の `Unmarshal` は
> 削除済み。配列フィールドを持つ構造体にスカラー版を渡すと、**そのプロパティが
> 応答に含まれるときだけ**実行時に落ちるデータ依存の故障になり、手書き golden では
> 検出できなかった (#126 / #136 / #137 が同型の事故)。選択肢を無くすことで塞いだ。

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
