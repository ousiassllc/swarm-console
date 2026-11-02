#!/usr/bin/env bash
# install.sh のテスト（NFR-TEST-18）。実際の GitHub にも systemd にも、実際の HOME にも触れない。
# curl・systemctl・loginctl・uname・sleep は PATH の先頭に置く偽物に差し替え、HOME はケースごとの一時ディレクトリにする。
#   使い方：bash scripts/install_test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL="$ROOT/scripts/install.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

pass=0
failed=0
check() { # check <名前> <条件を表すコマンド...>
  local name="$1"
  shift
  if "$@"; then
    pass=$((pass + 1))
  else
    failed=$((failed + 1))
    echo "FAIL: $CASE: $name" >&2
    [[ -z "${OUT:-}" ]] || echo "    | ${OUT//$'\n'/$'\n'    | }" >&2
  fi
}
has() { grep -qF -- "$1" "$2"; }
lacks() { ! grep -qF -- "$1" "$2"; }
out_has() { grep -qF -- "$1" <<<"$OUT"; }

# 偽物の置き場（PATH の先頭）を作る
make_stubs() { # make_stubs <ケースのフォルダ>
  local d="$1/stubs"
  mkdir -p "$d" "$1/state"
  cat >"$d/curl" <<'EOF'
#!/usr/bin/env bash
out="" fmt="" url=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -w) fmt="$2"; shift 2 ;;
    --proto|--proto-redir|--connect-timeout|--max-time|--max-filesize) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
echo "curl $url" >>"$STUB_LOG"
case "$url" in
  */releases/latest)
    [[ -f "$STUB_FIX/latest" ]] || exit 22
    [[ "$fmt" == *url_effective* ]] && printf 'https://github.com/ousiassllc/swarm-console/releases/tag/%s' "$(cat "$STUB_FIX/latest")"
    ;;
  */releases/download/*)
    rest="${url#*/releases/download/}"
    [[ -f "$STUB_FIX/$rest" ]] || exit 22
    cp "$STUB_FIX/$rest" "$out"
    ;;
  *) exit 6 ;;
esac
EOF
  cat >"$d/systemctl" <<'EOF'
#!/usr/bin/env bash
echo "systemctl $*" >>"$STUB_LOG"
case "$*" in
  *show-environment*) ;;
  *is-active*) [[ -f "$STUB_STATE/active" ]] ;;
  *"enable --now"*) [[ -n "${STUB_NEVER_ACTIVE:-}" ]] || touch "$STUB_STATE/active" ;;
esac
EOF
  cat >"$d/loginctl" <<'EOF'
#!/usr/bin/env bash
echo "loginctl $*" >>"$STUB_LOG"
case "$1" in
  show-user) cat "$STUB_STATE/linger" 2>/dev/null || echo no ;;
  enable-linger) echo yes >"$STUB_STATE/linger" ;;
esac
EOF
  cat >"$d/uname" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  -s) echo Linux ;;
  -m) echo "${STUB_UNAME_M:-x86_64}" ;;
esac
EOF
  printf '#!/bin/sh\nexit 0\n' >"$d/sleep"
  chmod +x "$d"/*
}

# 偽の最新リリース：make_release <フォルダ> <版> [ずらす: badhash|noline|dup|crlf|star|tab|sp3|sp1|badver]
make_release() {
  local fix="$1" tag="$2" mode="${3:-}" asset dir h
  asset="swarm-console_${tag}_linux_amd64"
  dir="$fix/$tag"
  mkdir -p "$dir"
  local ver="$tag"
  [[ "$mode" != badver ]] || ver=v9.9.9
  # shellcheck disable=SC2016 # 偽のバイナリの中身。$1 は実行時に展開する
  printf '#!/bin/sh\n[ "$1" = version ] && echo %s\nexit 0\n' "$ver" >"$dir/$asset"
  h="$(sha256sum "$dir/$asset")"
  h="${h%% *}"
  case "$mode" in
    badhash) printf '%064d  %s\n' 0 "$asset" >"$dir/SHA256SUMS" ;;
    noline) printf '%s  %s\n' "$h" "other_file" >"$dir/SHA256SUMS" ;;
    dup) printf '%s  %s\n%s  %s\n' "$h" "$asset" "$h" "$asset" >"$dir/SHA256SUMS" ;;
    crlf) printf '%s  %s\r\n' "$h" "$asset" >"$dir/SHA256SUMS" ;;
    star) printf '%s *%s\n' "$h" "$asset" >"$dir/SHA256SUMS" ;;
    tab) printf '%s\t%s\n' "$h" "$asset" >"$dir/SHA256SUMS" ;;
    sp3) printf '%s   %s\n' "$h" "$asset" >"$dir/SHA256SUMS" ;;
    sp1) printf '%s %s\n' "$h" "$asset" >"$dir/SHA256SUMS" ;;
    *) printf '%s  %s\n' "$h" "$asset" >"$dir/SHA256SUMS" ;;
  esac
  echo "$tag" >"$fix/latest"
}

# new_case <名前>：ケース用の HOME と偽物を用意する
new_case() {
  CASE="$1"
  C="$(mktemp -d "$WORK/case.XXXXXX")"
  make_stubs "$C"
  mkdir -p "$C/home" "$C/fix"
  export STUB_LOG="$C/log" STUB_STATE="$C/state" STUB_FIX="$C/fix"
  : >"$STUB_LOG"
  H="$C/home"
  BIN="$H/bin/swarm-console/swarm-console"
  UNIT="$H/.config/systemd/user/swarm-console.service"
  unset STUB_NEVER_ACTIVE STUB_UNAME_M
}
release() { make_release "$C/fix" "$@"; }

run_install() { # 終了コードを CODE、出力を OUT に
  CODE=0
  OUT="$(PATH="$C/stubs:$PATH" HOME="$H" USER=tester bash "$INSTALL" 2>&1)" || CODE=$?
}
mark() { MARK="$(wc -l <"$STUB_LOG")"; }
since_mark() { tail -n +"$((MARK + 1))" "$STUB_LOG" >"$C/log.since"; }
with_config() { mkdir -p "$H/.config/swarm-console" && echo 'listen_addr = "127.0.0.1:48120"' >"$H/.config/swarm-console/config.toml"; }
nothing_placed() { [[ ! -e "$H/bin" && ! -e "$UNIT" ]] && lacks "systemctl --user enable" "$STUB_LOG" && lacks "daemon-reload" "$STUB_LOG"; }
hash_of() { local h; h="$(sha256sum "$1")"; echo "${h%% *}"; }

# 1. 何も入っていない環境（設定なし）：置く・有効にする・起動しない・案内して終了コード 0
new_case "新規（設定なし）"
release v0.1.4
run_install
check "終了コード 0" test "$CODE" -eq 0
check "通常ファイルで実行できる" bash -c "[[ -f '$BIN' && ! -L '$BIN' && -x '$BIN' ]]"
check "版が最新" test "$("$BIN" version)" = v0.1.4
check "unit が deploy/ と同じ実ファイル" bash -c "[[ ! -L '$UNIT' ]] && cmp -s '$ROOT/deploy/swarm-console.service' '$UNIT'"
check "daemon-reload と enable（--now なし）" bash -c "grep -qF 'daemon-reload' '$STUB_LOG' && grep -qF 'enable swarm-console' '$STUB_LOG' && ! grep -qF -- '--now' '$STUB_LOG'"
check "リンガリングを設定" has "enable-linger tester" "$STUB_LOG"
check "設定ファイルの案内" out_has "設定ファイルがありません"
check "tmp ファイルが残らない" bash -c "[[ -z \"\$(find '$H' -name '.swarm-console*')\" ]]"

# 2. 設定あり：enable --now で active
new_case "新規（設定あり）"
with_config
release v0.1.4
run_install
check "終了コード 0" test "$CODE" -eq 0
check "enable --now" has "enable --now swarm-console" "$STUB_LOG"
check "active と出る" out_has "サービス: active"

# 3. もう一度流しても同じ（バイナリ・unit に触らない。daemon-reload・enable-linger もしない）
inode="$(stat -c %i "$BIN")"
mark
run_install
since_mark
check "終了コード 0" test "$CODE" -eq 0
check "バイナリを置き換えない" test "$(stat -c %i "$BIN")" = "$inode"
check "daemon-reload しない" lacks "daemon-reload" "$C/log.since"
check "enable-linger しない" lacks "enable-linger" "$C/log.since"
check "unchanged と出る" out_has "unchanged"
check "再起動しない" lacks "restart" "$C/log.since"

# 4. ハッシュが合わない（新規）：何も置かずに失敗
new_case "ハッシュ不一致（新規）"
release v0.1.4 badhash
run_install
check "失敗する" test "$CODE" -ne 0
check "ハッシュの不一致と出る" out_has "ハッシュが一致しません"
check "何も置かず systemd にも触れない" nothing_placed

# 5. ハッシュが合わない（入っている版がある）：入っている版に触れない
new_case "ハッシュ不一致（入れ替え）"
release v0.1.4
run_install
before="$(hash_of "$BIN")"
release v0.1.5 badhash
run_install
check "失敗する" test "$CODE" -ne 0
check "入っているバイナリは変わらない" test "$(hash_of "$BIN")" = "$before"

# 6. SHA256SUMS の形：行なし・重複・タブ・空白 3 つ・空白 1 つ（* なし）は失敗（FR-UPD-14）、CRLF・* は許す
for mode in noline dup tab sp3 sp1; do
  new_case "SHA256SUMS $mode"
  release v0.1.4 "$mode"
  run_install
  check "失敗する" test "$CODE" -ne 0
  check "何も置かない" nothing_placed
done
for mode in crlf star; do
  new_case "SHA256SUMS $mode"
  release v0.1.4 "$mode"
  run_install
  check "成功する" test "$CODE" -eq 0
done

# 7. バイナリの版がリリースの版と違う・ダウンロードできない・最新の版の形が違う
new_case "版の不一致"
release v0.1.4 badver
run_install
check "失敗する" test "$CODE" -ne 0
check "何も置かない" nothing_placed
new_case "アセットが無い"
release v0.1.4
rm "$C/fix/v0.1.4/swarm-console_v0.1.4_linux_amd64"
run_install
check "失敗する" test "$CODE" -ne 0
check "何も置かない" nothing_placed
new_case "最新の版の形が違う"
release v0.1.4
echo v1.2 >"$C/fix/latest"
run_install
check "失敗する" test "$CODE" -ne 0
check "何も置かない" nothing_placed

# 8. 新しい版が出たら入れ替える。動いているサービスは再起動しない
new_case "更新"
with_config
release v0.1.4
run_install
release v0.1.5
mark
run_install
since_mark
check "終了コード 0" test "$CODE" -eq 0
check "新しい版になる" test "$("$BIN" version)" = v0.1.5
check "updated と出る" out_has "updated"
check "再起動しない" lacks "restart" "$C/log.since"
check "再起動していない旨を案内" out_has "再起動していません"

# 9. 入っている版が最新リリース以上なら置き換えない（ダウングレードしない）
new_case "ダウングレードしない"
with_config
release v0.1.4
run_install
release v0.1.2
before="$(hash_of "$BIN")"
run_install
check "終了コード 0" test "$CODE" -eq 0
check "バイナリは変わらない" test "$(hash_of "$BIN")" = "$before"
check "kept と出る" out_has "kept"

# 10. 配置先がシンボリックリンクなら置き換えず失敗する（自己更新の条件）
new_case "シンボリックリンク"
release v0.1.4
mkdir -p "$H/bin/swarm-console" "$C/elsewhere"
echo old >"$C/elsewhere/real"
ln -s "$C/elsewhere/real" "$BIN"
run_install
check "失敗する" test "$CODE" -ne 0
check "リンクも中身も変わらない" bash -c "[[ -L '$BIN' && \"\$(cat '$C/elsewhere/real')\" == old ]]"
check "systemd に触れない" lacks "daemon-reload" "$STUB_LOG"

# 11. unit がリンクなら（リンク先が別内容でも deploy/ と同じ内容でも）実ファイルで書き直して daemon-reload。リンク先は変えない
for target in '[Unit]' "$(cat "$ROOT/deploy/swarm-console.service")"; do
  new_case "unit の書き直し（リンク先 ${target:0:6}）"
  release v0.1.4
  mkdir -p "$H/.config/systemd/user" "$C/elsewhere"
  echo "$target" >"$C/elsewhere/old.service"
  ln -s "$C/elsewhere/old.service" "$UNIT"
  run_install
  check "実ファイルになり deploy/ と同じ" bash -c "[[ ! -L '$UNIT' ]] && cmp -s '$ROOT/deploy/swarm-console.service' '$UNIT'"
  check "リンク先は変わらない" test "$(cat "$C/elsewhere/old.service")" = "$target"
  check "unit: written と出る" out_has "written"
  check "daemon-reload が 1 回" test "$(grep -cF daemon-reload "$STUB_LOG")" -eq 1
done

# 12. 対応外の環境・起動しないサービス・リンガリングがすでに yes
new_case "対応外の環境"
release v0.1.4
export STUB_UNAME_M=aarch64
run_install
check "失敗する" test "$CODE" -ne 0
check "何も置かない" nothing_placed
new_case "active にならない"
with_config
release v0.1.4
export STUB_NEVER_ACTIVE=1
run_install
check "失敗する" test "$CODE" -ne 0
check "journalctl の案内" out_has "journalctl"
new_case "リンガリングがすでに yes"
release v0.1.4
echo yes >"$STUB_STATE/linger"
run_install
check "enable-linger しない" lacks "enable-linger" "$STUB_LOG"

echo "install_test: ${pass} passed, ${failed} failed"
[[ "$failed" -eq 0 ]]
