# ADR-0001: PowerShell に依存しない理由

> テーマ単位のファイル。決定が変わったら**末尾に追記**する(過去を書き換えない)。
> 詳しい運用は [README.md](README.md) を参照。

## 現在の決定

**Hyper-V を CIM (WS-Management) で直接操作し、PowerShell を一切介さない。**
これは既存の Terraform provider 2 つとの唯一かつ最大の差分であり、このライブラリが存在する理由そのもの。

| 項目 | 内容 |
|---|---|
| 状態 | 採用 |
| 最終更新 | 2026-08-27 |
| 関連 | `wsman/`, `hyperv/`, `docs/specs/2026-04-07-hyperv-cim-bindings-design.md` |

---

## 2026-04-07: CIM ネイティブ実装を選ぶ

### 背景

Terraform から Hyper-V を管理したかったが、既存の provider はいずれも
**Go プロセスから PowerShell を起動し、その標準入出力を経由して Hyper-V を操作する**構造になっていた。

この構造には実務上の困りごとがある。

- **ホスト側に PowerShell 実行環境が要る。** バージョン差(Windows PowerShell 5.1 / PowerShell 7)で挙動が変わる
- **エラーが文字列で返る。** PowerShell の例外メッセージをパースして分岐するため、ホストの表示言語が変わると壊れる
- **型が失われる。** `uint16` の列挙値が文字列化され、往復で情報が落ちる
- **失敗の切り分けが難しい。** 「WinRM が落ちたのか」「PS スクリプトが落ちたのか」「Hyper-V が拒否したのか」が同じ形のエラーになる

一方、Hyper-V の実体は **WMI/CIM クラス群 (`root/virtualization/v2` の `Msvm_*`)** であり、
PowerShell の Hyper-V モジュール自体がその薄いラッパーにすぎない。
WS-Management (WinRM が話しているプロトコル) は SOAP/XML なので、Go から直接話せる。

### 選択肢と評価

| 案 | 内容 | 判断 |
|---|---|---|
| A: CIM を Go から直接呼ぶ | WS-Man/SOAP を自前実装し `Msvm_*` を型付きで扱う | ✅ 採用 |
| B: taliesins/terraform-provider-hyperv を使う | 既存のデファクト。PowerShell 経由 | ❌ 却下: 開発が停滞 |
| C: windsorcli/terraform-provider-hyperv を使う | 活発だが PowerShell 埋め込み方式 | ❌ 却下: PS 依存が残る |
| D: 既存 provider に PR を送る | PS 経路を CIM 経路に置き換える | ❌ 却下: 事実上の全面書き換え |

### 根拠

いずれも 2026-08-27 に一次確認した。

**B: taliesins/terraform-provider-hyperv**

| 項目 | 値 |
|---|---|
| 最新コミット | 2024-03-27 |
| open issue | 21 件(open PR 9 件を含めると 30 件) |
| star | 263 |
| アーカイブ | されていない |

star 数から広く使われていることは分かるが、**2 年以上コミットがない**。
アーカイブされていないため「メンテナンス停止」と断定はできないが、
新しい Windows / Terraform への追随を期待できる状態ではない。

**C: windsorcli/terraform-provider-hyperv**

2026-08-27 時点で当日にも push されている活発なプロジェクト。ただし README が実装方式を明記している。

> "Embedded PowerShell with a JSON contract. Each operation ships an embedded `.ps1` through the chosen transport and round-trips JSON via stdin/stdout."

必要要件として **Windows PowerShell 5.1**(Windows に同梱)を挙げ、PowerShell 7.4+ も
サポート対象としている(必須ではない)。transport は local PowerShell / OpenSSH / WinRM から選ぶ。
つまり JSON 契約で型崩れは改善しているが、**PowerShell 実行環境への依存そのものは残る**。
本 ADR が避けたかった点は解消されていない。

なお windsorcli は pre-1.0 であり、README も
「Schema, attribute names, and behavior may change between minor versions until `v1.0.0` ships.」
と明記している。

**A を採ると何が得られるか**

- 中間層が消えるので、失敗したときに **どの層で失敗したか**が型で分かる
  (`*wsman.Fault` = Hyper-V が拒否した / transport エラー = 通信 / unmarshal エラー = 型の不一致)
- CIM の列挙値を `uint16` のまま扱える(ADR-0002)
- ホスト側の表示言語・PowerShell バージョンに依存しない
- 依存ライブラリが 2 つで済む(`go.mod` 参照)

### 却下した理由(重要)

**D(既存 provider への PR)を選ばなかったのは、変更の規模が実質的な作り直しになるため。**
PS 経路を前提にした API 境界・エラー型・テストが全面的に変わるので、
上流にとってもレビュー不能な差分になる。フォークして別物にする方が双方にとって正直だと判断した。

**B・C は「悪い実装だから却下」ではない。** どちらも PowerShell を前提に置けば妥当な設計で、
特に C は活発に開発されている。**PowerShell 依存を許容できるなら C は合理的な選択肢**であり、
本プロジェクトが要らない人も多い。ここで却下したのは前提が違うからにすぎない。

### この決定のコスト(正直に書く)

CIM を直接叩くと、PowerShell が肩代わりしてくれていた面倒がすべて自分に返ってくる。

- **仕様が MOF にしか書いていない。** 一次資料の確認が必須になった(ADR-0002)
- **メソッドが非同期で返る。** Job オブジェクトをポーリングする層が要る(`hyperv/job.go`)
- **成功を返して何もしないことがある。** これが最大の落とし穴で、専用のテスト戦略が必要になった(ADR-0003)
- **NTLM の並行実行で 401 になる。** 接続プールを自作した(ADR-0004)

これらは PS 経由なら踏まなかった。**「PS を外す」ことの対価**として記録しておく。

### 見直しの条件

- [ ] windsorcli が PowerShell を介さない経路を提供した → 自作の意義が薄れるので乗り換えを検討する
- [ ] Microsoft が Hyper-V の管理 API として CIM 以外の第一級インターフェース(REST 等)を出した
- [ ] 必要な機能が揃い、これ以上 CIM を掘る動機がなくなった → 機能追加を止めて保守のみに移る
