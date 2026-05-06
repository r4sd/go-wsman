#!/bin/sh
#
# install-hooks.sh: ローカルリポジトリの Git hooks を有効化する
#
# core.hooksPath を .githooks/ に向けることで、リポジトリ内のフックを
# .git/hooks/ にコピーせず利用する。clone 直後・新環境セットアップ時に 1 回実行する。

set -e

REPO_ROOT=$(git rev-parse --show-toplevel)
cd "$REPO_ROOT"

git config core.hooksPath .githooks

echo "✓ core.hooksPath = .githooks に設定しました"
echo "  以降のコミット時に .githooks/pre-commit が自動実行されます"
echo ""
echo "無効化したい場合:"
echo "  git config --unset core.hooksPath"
