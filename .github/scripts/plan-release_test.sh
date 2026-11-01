#!/usr/bin/env bash
# plan-release.sh の表駆動テスト（NFR-TEST-15）。ネットワークは使わない。
# ケースごとに使い捨ての git リポジトリ（古いコミット old と main の先頭 head）を作り、タグを付けて plan-release.sh を動かす。
#   使い方：bash .github/scripts/plan-release_test.sh
set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/plan-release.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

pass=0
failed=0

# 使い捨てのリポジトリ：old ← head（origin/main の先頭）。
# $1: old に付ける既存のタグ（空白区切り）、$2: head に付けるタグ
make_repo() {
  local dir="$1" tag
  git init -q "$dir"
  git -C "$dir" -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m old
  git -C "$dir" rev-parse HEAD >"$dir/.old"
  git -C "$dir" -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m head
  git -C "$dir" rev-parse HEAD >"$dir/.head"
  git -C "$dir" update-ref refs/remotes/origin/main HEAD
  for tag in $2; do git -C "$dir" tag "$tag" "$(cat "$dir/.old")"; done
  for tag in $3; do git -C "$dir" tag "$tag" "$(cat "$dir/.head")"; done
}

# run_case 名前 期待 old のタグ head のタグ 対象のコミット(old|head) [環境変数 KEY=VAL ...]
#   期待：「release=true tag=vX.Y.Z」「release=false」「error」（終了コードが 0 でなく、release=true を書かない）
run_case() {
  local name="$1" want="$2" old_tags="$3" head_tags="$4" target="$5" dir out code=0 got
  shift 5
  dir="$(mktemp -d "$WORK/repo.XXXXXX")"
  make_repo "$dir" "$old_tags" "$head_tags"
  out="$dir/.out"
  : >"$out"
  (
    cd "$dir"
    export GITHUB_OUTPUT="$out" RELEASE_SHA GITHUB_REPOSITORY=o/r
    RELEASE_SHA="$(cat ".$target")"
    # 既定は「main の push の CI が成功」。ケースごとに上書きする
    export GITHUB_EVENT_NAME=workflow_run WORKFLOW_RUN_CONCLUSION=success WORKFLOW_RUN_EVENT=push
    export WORKFLOW_RUN_HEAD_BRANCH=main WORKFLOW_RUN_HEAD_REPO=o/r
    for kv in "$@"; do export "${kv?}"; done
    bash "$SCRIPT"
  ) >"$dir/.log" 2>&1 || code=$?

  got="$(tr '\n' ' ' <"$out" | sed 's/ $//')"
  if [ "$want" = error ]; then
    if [ "$code" -ne 0 ] && ! grep -q '^release=true' "$out"; then
      pass=$((pass + 1))
      return
    fi
  elif [ "$code" -eq 0 ] && [ "$got" = "$want" ]; then
    pass=$((pass + 1))
    return
  fi
  failed=$((failed + 1))
  echo "FAIL: $name: want '$want', got '$got' (exit $code)"
  sed 's/^/    /' "$dir/.log"
}

# --- workflow_run（自動のタグ） ---
run_case "タグなし → v0.1.0" "release=true tag=v0.1.0" "" "" head
run_case "v0.1.0 の次は v0.1.1" "release=true tag=v0.1.1" "v0.1.0" "" head
run_case "v0.1.9 の次は v0.1.10（数として比較）" "release=true tag=v0.1.10" "v0.1.0 v0.1.9" "" head
run_case "v0.1.10 があれば v0.1.9 より大きい" "release=true tag=v0.1.11" "v0.1.9 v0.1.10" "" head
run_case "手で打った v1.0.0 の次は v1.0.1" "release=true tag=v1.0.1" "v0.1.7 v1.0.0" "" head
run_case "v0.9.0 と v0.10.0 は数として比較" "release=true tag=v0.10.1" "v0.9.5 v0.10.0" "" head
run_case "形の合わないタグ（プレリリース等）は最新に数えない" "release=true tag=v0.1.1" "v0.1.0 v9.9.9-rc1 v9.9 1.2.3" "" head
run_case "main の先頭でないコミットは何もしない" "release=false" "" "" old
run_case "main の先頭に v* タグが既にあれば何もしない" "release=false" "v0.1.0" "v0.1.1" head
run_case "形の合わない v* タグでも、先頭に v* タグがあれば何もしない" "release=false" "" "v1.0.0-rc1" head
run_case "CI が失敗なら何もしない" "release=false" "" "" head WORKFLOW_RUN_CONCLUSION=failure
run_case "push でない CI（PR）は何もしない" "release=false" "" "" head WORKFLOW_RUN_EVENT=pull_request
run_case "main 以外のブランチの CI は何もしない" "release=false" "" "" head WORKFLOW_RUN_HEAD_BRANCH=feature
run_case "フォークの CI は何もしない" "release=false" "" "" head WORKFLOW_RUN_HEAD_REPO=evil/r
run_case "対象のリポジトリが空なら何もしない" "release=false" "" "" head WORKFLOW_RUN_HEAD_REPO=
run_case "main の ref が無ければエラー" "error" "" "" head MAIN_REF=origin/none

# --- タグ push（手で打ったタグ。マイナー・メジャーを上げる） ---
run_case "手のタグ v1.0.0（既存より大きい）" "release=true tag=v1.0.0" "v0.1.9" "v1.0.0" head GITHUB_EVENT_NAME=push GITHUB_REF_NAME=v1.0.0
run_case "手のタグ v0.2.0（タグなしの次）" "release=true tag=v0.2.0" "" "v0.2.0" head GITHUB_EVENT_NAME=push GITHUB_REF_NAME=v0.2.0
run_case "手のタグ v0.1.10 は v0.1.9 より大きい" "release=true tag=v0.1.10" "v0.1.9" "v0.1.10" head GITHUB_EVENT_NAME=push GITHUB_REF_NAME=v0.1.10
run_case "手のタグが既存の最大以下（小さい）" "error" "v1.0.0" "v0.9.0" head GITHUB_EVENT_NAME=push GITHUB_REF_NAME=v0.9.0
run_case "手のタグが既存の最大以下（v0.1.9 < v0.1.10）" "error" "v0.1.10" "v0.1.9" head GITHUB_EVENT_NAME=push GITHUB_REF_NAME=v0.1.9
run_case "手のタグ：プレリリース形は拒否" "error" "" "v1.0.0-rc1" head GITHUB_EVENT_NAME=push GITHUB_REF_NAME=v1.0.0-rc1
run_case "手のタグ：ビルド付きは拒否" "error" "" "v1.0.0+b1" head GITHUB_EVENT_NAME=push GITHUB_REF_NAME=v1.0.0+b1
run_case "手のタグ：v 無しは拒否" "error" "" "1.0.0" head GITHUB_EVENT_NAME=push GITHUB_REF_NAME=1.0.0
run_case "手のタグ：vX.Y は拒否" "error" "" "v1.0" head GITHUB_EVENT_NAME=push GITHUB_REF_NAME=v1.0
run_case "手のタグ：vX は拒否" "error" "" "v1" head GITHUB_EVENT_NAME=push GITHUB_REF_NAME=v1
run_case "手のタグ：先頭の 0 は拒否" "error" "" "v01.2.3" head GITHUB_EVENT_NAME=push GITHUB_REF_NAME=v01.2.3
run_case "手のタグ：末尾に文字は拒否" "error" "" "v1.0.0x" head GITHUB_EVENT_NAME=push GITHUB_REF_NAME=v1.0.0x

# --- その他 ---
run_case "未対応のイベントはエラー" "error" "" "" head GITHUB_EVENT_NAME=pull_request

echo "plan-release_test: $pass passed, $failed failed"
[ "$failed" -eq 0 ]
