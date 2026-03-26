# Contributing Guidelines

このリポジトリへの貢献方法について説明します。

## GitHub Issue/PR 管理規約

### 基本方針

**個人リポジトリ**:
- ✅ Issue/PR は日本語でOK
- ✅ リポジトリがあるタスク → Issue作成（詳細管理）
- ✅ リポジトリがないアイデア → ローカル（docs/Notion）で保管
- ✅ 緊急修正も事前にIssue作成（個人開発は急がない）

**OSS リポジトリ**:
- ✅ そのリポジトリの言語・規約に従う

### タスク管理: ハイブリッド方式

**ローカル（docs/Notion）**: 概要・アイデア管理
- ✅ プロジェクト概要
- ✅ リポジトリ未作成のアイデア
- ✅ 週次/月次目標
- ✅ GitHub Issue への参照（"ISSUE #123 参照"）

**GitHub Issues**: 実装詳細管理
- ✅ 具体的なタスク（リポジトリあり）
- ✅ バグ報告
- ✅ 機能要望

**運用例**:
```
docs/ideas.md (ローカル):
  - Discord MCP レート制限対応 → ISSUE #6 参照
  - Hermes AI分析機能（構想中、リポジトリなし）
```

### Issue とコミットの紐付け

**コミットメッセージ**:
```bash
# Issue番号を含める（日本語OK）
fix: EPIPEエラー修正 (#6)

# 自動クローズ
Closes #6
```

**自動クローズキーワード**: `Closes #123`, `Fixes #123`, `Resolves #123`

### Issue テンプレート

プロジェクトには以下のIssueテンプレートが用意されています：

- `.github/ISSUE_TEMPLATE/bug_report.md` - バグ報告用
- `.github/ISSUE_TEMPLATE/feature_request.md` - 機能要望用

新しいIssueを作成する際は、該当するテンプレートを選択してください。

## Pull Request の作成

1. **ブランチを作成**
   ```bash
   git checkout -b feature/your-feature-name
   # または
   git checkout -b fix/your-bug-fix
   ```

2. **変更をコミット**
   ```bash
   git add .
   git commit -m "feat: 新機能の追加 (#123)"
   ```

3. **リモートにプッシュ**
   ```bash
   git push -u origin feature/your-feature-name
   ```

4. **Pull Request を作成**
   ```bash
   gh pr create --title "タイトル" --body "説明"
   ```

## ワークフローのカスタマイズ

このテンプレートには複数のGitHub Actions ワークフローが含まれています。
プロジェクトの種類に応じて、必要なワークフローのみを残してください。

詳細は [README.md](README.md) を参照してください。

## ライセンス

このテンプレートはそのままプロジェクトで使用できます。
