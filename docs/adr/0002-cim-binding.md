# ADR-0002: CIM クラスのバインディング方式

> テーマ単位のファイル。決定が変わったら**末尾に追記**する(過去を書き換えない)。
> 詳しい運用は [README.md](README.md) を参照。

## 現在の決定

**`cim:"PropertyName"` タグ + reflection で `map[string]string` を Go 構造体にマッピングする。**
フィールド名・型・列挙値は **Microsoft 公式 MOF を一次資料**として決め、CI で機械照合する。

| 項目 | 内容 |
|---|---|
| 状態 | 採用 |
| 最終更新 | 2026-08-27 |
| 関連 | `hyperv/unmarshal.go`, `hyperv/types.go`, `hyperv/cim_compliance_test.go`, `hyperv/testdata/mof/` |

---

## 2026-04-07: reflection + struct tag を選ぶ

### 背景

Terraform provider の代替を目指すと、扱う CIM クラスは数十に及ぶ。
`Msvm_ComputerSystem`, `Msvm_VirtualSystemSettingData`, `Msvm_ProcessorSettingData` …
それぞれが十数〜数十のプロパティを持つ。

WS-Man のレスポンスは XML だが、プロパティ部分を取り出せば実質 `map[string]string` になる。
これを Go の構造体に移す方法を決める必要があった。

### 選択肢と評価

| 案 | 内容 | 判断 |
|---|---|---|
| A: reflection + struct tag | `cim:"ElementName"` タグを見て代入 | ✅ 採用 |
| B: クラスごとに手書き変換 | `func (m *Msvm_X) fromMap(...)` を各クラスに書く | ❌ 却下: 数十クラスでスケールしない |
| C: MOF からコード生成 | MOF をパースして Go 構造体を自動生成 | ❌ 却下(保留): MVP では過剰 |

### 決定

A を採用。`hyperv/unmarshal.go` の `Unmarshal(props map[string]string, v interface{}) error` が
`cim` タグを見て型変換する。対応型は `string` / `bool` / `int` / `int64` / `uint16` / `uint32` / `uint64`。

動作ルール:

- `cim` タグのないフィールドはスキップする
- map にないプロパティは**ゼロ値**にする(エラーにしない)。CIM は返さないプロパティがあるため
- **型変換に失敗したら fail-fast でエラーを返す。** 部分的な結果は返さない
- エラーメッセージにフィールド名と CIM プロパティ名の両方を含める

### 却下した理由

**B(手書き)** は型安全だが、クラスを 1 つ増やすたびに定型コードが数十行増える。
プロパティを 1 つ足すのに 2 箇所(構造体とパーサ)を直す必要があり、片方を忘れる事故が起きる。

**C(コード生成)** は本来もっとも堅い。ただし MVP の時点では、

- パースすべき MOF の形式調査に着手コストがかかる
- 実際に使うプロパティは各クラスの一部で、全プロパティを生成しても使わない
- 生成器自体のテストが必要になる

ため見送った。**却下ではなく保留**であり、扱うクラスがさらに増えたら再検討する。

### reflection の弱点をどう埋めたか

reflection は「タグの綴りを間違えても実行時まで気付かない」のが弱点で、
`cim:"ElementNmae"` のような typo は**ゼロ値として静かに通ってしまう**。

これを 2 つの仕組みで塞いだ。

1. **golden file テスト** — 実機からダンプした XML を食わせ、期待値と突き合わせる(ADR-0003)
2. **MOF 照合テスト** — 下記

---

## 2026-04〜: MOF を一次資料と定め、CI で照合する

### 背景

CIM クラスのプロパティ名・型・列挙値の出典をどこに置くかは、当初あいまいだった。
実際には Issue の記述や他プロジェクトのコードを参考にすることがあり、
そこから**間違った定義がそのままテストごと固定される**事故が起きた。

### 決定

**一次資料は Microsoft 公式 MOF ドキュメントのみ。** URL はアンダースコアを除いた全小文字のスラグ形式。

```
https://learn.microsoft.com/en-us/windows/win32/hyperv_v2/msvm-<class-slug>
例: Msvm_VirtualHardDiskSettingData → msvm-virtualharddisksettingdata
```

- `cim:"..."` タグは **MOF のプロパティ名と完全一致**させる(Go 側の識別子名は別でよい)
- **Issue の記述・他の provider の Go コード・他言語のライブラリは二次情報として扱い、根拠にしない**
- 新規クラスを足すときは `hyperv/testdata/mof/{class_snake_case}.txt` に
  **構造体が参照するプロパティだけ**を保存する(網羅は不要)
- `hyperv/cim_compliance_test.go` が reflection で構造体タグと突き合わせ、CI で落とす

### 根拠

二次情報を信じて失敗した実例がある。

- **`VirtualSystemTypeSnapshotRealized` 定数が誤っていた**(2026-08-01)。
  正しくは `Microsoft:Hyper-V:Snapshot:Realized` で、スナップショットには `System:` が入らない。
  この誤りにより `ListVmCheckpoints` が実機で常にゼロ件を返していたが、
  手書きの golden file が同じ誤りを含んでいたため**テストは緑のまま**だった。

### MOF だけでは決まらないこともある

MOF は「プロパティが存在するか」「型は何か」は教えてくれるが、
**そのプロパティを書き込めるか**は教えてくれない。

> `hyperv/vm.go` のコメント: 「クリア対象は実機検証で決めている。MOF の Access type は
> ModifySystemSettings が受理するか を判別できない」

このため運用は **MOF による型の裏取り + 実機ダンプによる挙動確認の二段構え**になっている。
コード中の「実機確認済み (日付)」コメントはこの二段目の記録。

### 見直しの条件

- [ ] 扱う CIM クラスが 30 を超えた → コード生成(案 C)を再検討する
- [ ] MOF 照合テストが未整備の既存クラスで typo 由来のバグが出た → 遡及して fixture を足す
