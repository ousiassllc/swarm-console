#!/usr/bin/env bash
# release.yml の plan ジョブ：リリースするか、付けるタグは何かを決める。
# 結果は $GITHUB_OUTPUT へ release=true|false、release=true のときは tag=vX.Y.Z も書く。
# 手で打ったタグが規則に合わないときはエラーで終わる（リリースしない。P-53）。
#
# 入力（環境変数）：
#   GITHUB_EVENT_NAME        push（手で打ったタグ）| workflow_run（main の CI 成功）
#   GITHUB_OUTPUT            結果の書き先（必須）
#   RELEASE_SHA              リリースするコミット（workflow_run は CI が通ったコミット、タグ push はタグのコミット）
#   GITHUB_REF_NAME          push のとき、打ったタグ
#   WORKFLOW_RUN_CONCLUSION  workflow_run のとき、CI の結論（success だけ通す）
#   WORKFLOW_RUN_EVENT       同、CI を起こしたイベント（push だけ通す。PR は通さない）
#   WORKFLOW_RUN_HEAD_BRANCH 同、CI の対象のブランチ（main だけ通す）
#   WORKFLOW_RUN_HEAD_REPO   同、CI の対象のリポジトリ（GITHUB_REPOSITORY と同じだけ通す。フォークは通さない）
#   GITHUB_REPOSITORY        このリポジトリ
#   MAIN_REF                 main の先頭を指す ref（既定 origin/main）
# 既存のタグと main の先頭は、カレントディレクトリの git リポジトリから読む（テストでは使い捨てのリポジトリを作る）。
#
# main の履歴にあるか・コミットの CI が成功したかは、ここでは見ない（release.yml の verify ジョブが見る。
# 後者は GitHub の API が要るため）。
set -euo pipefail

: "${GITHUB_EVENT_NAME:?}" "${GITHUB_OUTPUT:?}" "${RELEASE_SHA:?}"
MAIN_REF="${MAIN_REF:-origin/main}"

# 版の形。プレリリース・`v` 無し・`vX.Y`・先頭の 0（v01.2.3）は不可
readonly VERSION_RE='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'

skip() {
  echo "::notice::$1"
  echo "release=false" >>"$GITHUB_OUTPUT"
  exit 0
}

fail() {
  echo "::error::$1"
  exit 1
}

# 形の合う既存のタグを、版の大きい順に（数として比較する。v0.1.10 は v0.1.9 より大きい）
versions_desc() {
  git tag --list 'v*' | { grep -E "$VERSION_RE" || true; } | sort -t. -k1.2,1nr -k2,2nr -k3,3nr
}

release() {
  echo "::notice::releasing $1"
  {
    echo "release=true"
    echo "tag=$1"
  } >>"$GITHUB_OUTPUT"
  exit 0
}

plan_tag_push() {
  local tag="${GITHUB_REF_NAME:?}" max
  [[ "$tag" =~ $VERSION_RE ]] || fail "tag $tag is not vX.Y.Z (no prerelease, with the leading v): no release"
  # 打ったタグ以外の最大。これより大きくないと、GitHub の「最新のリリース」（日時順）が古い版に戻る
  max="$(versions_desc | { grep -vxF "$tag" || true; } | sed -n 1p)"
  if [ -n "$max" ] && [ "$(printf '%s\n%s\n' "$max" "$tag" | sort -t. -k1.2,1n -k2,2n -k3,3n | tail -n 1)" != "$tag" ]; then
    fail "tag $tag is not greater than the existing $max: tag a higher version (the latest release must be the highest)"
  fi
  release "$tag"
}

plan_workflow_run() {
  local head latest major minor patch
  [ "${WORKFLOW_RUN_CONCLUSION:-}" = success ] || skip "CI did not succeed (${WORKFLOW_RUN_CONCLUSION:-unknown}): no release"
  [ "${WORKFLOW_RUN_EVENT:-}" = push ] || skip "CI ran for ${WORKFLOW_RUN_EVENT:-unknown}, not a push: no release"
  [ "${WORKFLOW_RUN_HEAD_BRANCH:-}" = main ] || skip "CI ran for branch ${WORKFLOW_RUN_HEAD_BRANCH:-unknown}, not main: no release"
  if [ -z "${GITHUB_REPOSITORY:-}" ] || [ "${WORKFLOW_RUN_HEAD_REPO:-}" != "$GITHUB_REPOSITORY" ]; then
    skip "CI ran for ${WORKFLOW_RUN_HEAD_REPO:-unknown}, not this repository: no release"
  fi

  head="$(git rev-parse --verify "$MAIN_REF^{commit}")" || fail "cannot resolve $MAIN_REF"
  [ "$head" = "$RELEASE_SHA" ] || skip "$RELEASE_SHA is not the tip of $MAIN_REF any more: the run for the newer commit releases"
  [ -z "$(git tag --points-at "$RELEASE_SHA" --list 'v*')" ] || skip "$RELEASE_SHA already has a version tag: nothing new to release"

  latest="$(versions_desc | sed -n 1p)"
  if [ -z "$latest" ]; then
    release v0.1.0
  fi
  IFS=. read -r major minor patch <<<"${latest#v}"
  release "v$major.$minor.$((patch + 1))"
}

case "$GITHUB_EVENT_NAME" in
push) plan_tag_push ;;
workflow_run) plan_workflow_run ;;
*) fail "unsupported event $GITHUB_EVENT_NAME" ;;
esac
