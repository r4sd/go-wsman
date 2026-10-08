#!/usr/bin/env bash
#
# 公開リポジトリに出してはいけない値を走査する。
#
# 使い方:
#   scan_forbidden.sh files           追跡ファイルの内容を走査 (git リポジトリ内で実行)
#   scan_forbidden.sh text < 入力     標準入力のテキストを走査 (コミットメッセージ / PR 本文)
#
# 終了コード:
#   0  問題なし
#   1  検出した
#   2  検査が成立しなかった (引数誤り・git の異常終了など)
#
# 🔴 **検出した値は出力しない。** 漏れていた場合に CI のログで二次公開になる。
#   files モードは該当ファイルのパスだけ、text モードは種別だけを出す。
#
# 🔴 **コミットメッセージと PR 本文に「検査パターンそのもの」を書かない。**
#   text モードはそれらも走査するので、負のテストの表を本文に貼ると自分で落ちる
#   (実際にやった)。例を書きたいときは「伏せ字 + 区切り + 生の値の形」のように
#   日本語で説明する。実際の入力は internal/guard のテストに置いてある。
#
# 🔴 **fail-open を避ける。** git grep の終了コードは 0=一致あり / 1=一致なし /
#   それ以外=異常。異常を「一致なし」と同じ扱いにすると、pathspec の書き間違いや
#   リポジトリ破損で**検査が静かに通る**。rc > 1 は 2 で落とす。
set -uo pipefail

# 既知の実環境識別子 (denylist)。新しい実環境を触ったらここに足す。
#
# ⚠️ **この方式は漏洩を防がない。** 既知の値の再混入を止めるだけで、未登録の
#   実環境から採った値は素通りする。一次の防御は記録器の伏せ字
#   (internal/guard + wsman/record.go) 側にある。
#
# ⚠️ **ここに書いた値はこの公開ファイル自身が公開する。** だからユーザー名のような
#   まだ公開していない値は書かない。そちらは下の「部分伏せ字」検査が形で捕まえる。
DENY_RE='DESKTOP-KAKEF4K|k8s-cp-0[0-9]|k8s-worker-0[0-9]|27BBD2D0-EA18|2C709429-A8C3|92617271-7416'

# 部分的に伏せられた値。
#
# 記録器は "<ホスト名>\<ユーザー名>" のうちホスト名側しか伏せていなかった (#188)。
# 結果 "scrubbed-1\<実ユーザー名>" の形で公開リポジトリに出た。
#
# 候補: scrubbed-N の後ろにバックスラッシュ 1 個以上と、引用符 / 山括弧 / 空白を
#   含まない文字列。`<` を除くのは XML の "</p:Owner>" を巻き込まないため。
PARTIAL_CAND_RE='scrubbed-[0-9]+\\+[^"< ]+'

# 許可される形。別の伏せ字か、伏せ字プレースホルダ。
#
# バックスラッシュを 1 個以上許すのは、Go の interpreted string で
# "scrubbed-1\\scrubbed-2" と書かれると**ファイル上は 2 個**になるため。
# ここを 1 個固定にすると、Owner を assert するテストを書いた瞬間に CI が落ち、
# 「通すために正規表現を緩める」圧力になる。
#
# &lt;user&gt; は XML エスケープされた伏せ字表記。素の <user> は候補側で
# `<` を除いているので、そもそも候補にならない (= 常に許可される)。
PARTIAL_ALLOW_RE='^scrubbed-[0-9]+\\+(scrubbed-[0-9]+|&lt;[a-z]+&gt;)$'

# 走査から除くパス (この検査自身は denylist の値を持っている)。
EXCLUDE=':(exclude).github/scripts/scan_forbidden.sh'

die_internal() {
  echo "::error::検査が成立しませんでした: $1"
  exit 2
}

# git_grep_or_die は rc > 1 を異常として扱う。出力は標準出力へ。
git_grep_or_die() {
  local out rc
  out="$(git grep "$@")"
  rc=$?
  if [ "$rc" -gt 1 ]; then
    die_internal "git grep が rc=$rc で終了 ($*)"
  fi
  printf '%s' "$out"
}

scan_files() {
  local found=0

  # 1. RFC 1918 アドレス
  local ips
  ips="$(git_grep_or_die -lIE '(^|[^0-9.])(10\.[0-9]{1,3}|192\.168|172\.(1[6-9]|2[0-9]|3[01]))\.[0-9]{1,3}\.[0-9]{1,3}([^0-9.]|$)' -- . "$EXCLUDE")"
  if [ -n "$ips" ]; then
    echo "::error::プライベート IP アドレスが含まれています。RFC 5737 の例示用アドレスかプレースホルダに置き換えてください。"
    printf '%s\n' "$ips" | sed 's/^/  /'
    found=1
  fi

  # 2. 既知の実環境識別子
  local ids
  ids="$(git_grep_or_die -lIiE "$DENY_RE" -- . "$EXCLUDE")"
  if [ -n "$ids" ]; then
    echo "::error::実環境の識別子 (ホスト名 / VM 名 / VM GUID) が含まれています。"
    printf '%s\n' "$ids" | sed 's/^/  /'
    found=1
  fi

  # 3. 部分的に伏せられた値。**ファイル単位で判定し、値は出さない。**
  local cands f offenders=""
  cands="$(git_grep_or_die -lIE "$PARTIAL_CAND_RE" -- . "$EXCLUDE")"
  if [ -n "$cands" ]; then
    while IFS= read -r f; do
      [ -n "$f" ] || continue
      if git grep -hoE "$PARTIAL_CAND_RE" -- "$f" | grep -qvE "$PARTIAL_ALLOW_RE"; then
        offenders="${offenders}${f}"$'\n'
      fi
    done <<< "$cands"
  fi
  if [ -n "$offenders" ]; then
    echo "::error::片側だけ伏せられた値があります (scrubbed-N + 生の値)。両側を伏せてください。"
    printf '%s' "$offenders" | sed 's/^/  /'
    found=1
  fi

  if [ "$found" = 0 ]; then
    echo "OK: 追跡ファイルに禁止値なし"
  fi
  return "$found"
}

scan_text() {
  local text found=0
  text="$(cat)"
  if [ -z "$text" ]; then
    echo "OK: 入力が空"
    return 0
  fi

  if printf '%s' "$text" | grep -qIiE "$DENY_RE"; then
    echo "::error::テキストに実環境の識別子が含まれています。"
    found=1
  fi
  if printf '%s' "$text" | grep -hoE "$PARTIAL_CAND_RE" | grep -qvE "$PARTIAL_ALLOW_RE"; then
    echo "::error::テキストに片側だけ伏せられた値があります (scrubbed-N + 生の値)。"
    found=1
  fi

  if [ "$found" != 0 ]; then
    echo "::notice::該当箇所は手元で確認してください (内容はここに出していません)。"
  else
    echo "OK: テキストに禁止値なし"
  fi
  return "$found"
}

case "${1:-}" in
files) scan_files ;;
text) scan_text ;;
*) die_internal "使い方: $0 {files|text}" ;;
esac
