#!/usr/bin/env bash
# make install の実体（FR-INS）。GitHub Releases の最新版を取り、SHA256SUMS で照合してから
# ~/bin/swarm-console/swarm-console に置き、systemd のユーザーサービスとして常駐させる。
# 手元ビルドは使わない。何回流しても同じ結果になる（仕様は docs/requirements/functional-install.md）。
#   使い方：make install    （リポジトリの直下で。deploy/swarm-console.service をそのリポジトリから読む）
set -euo pipefail

REPO="ousiassllc/swarm-console"
RELEASES="https://github.com/${REPO}/releases"
SERVICE="swarm-console"
TAG_RE='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UNIT_SRC="$ROOT/deploy/${SERVICE}.service"

die() {
  echo "install: エラー: $*" >&2
  exit 1
}
say() { echo "install: $*"; }

# HOME は絶対パスでなければならない（置き場も unit の %h も HOME から決まる）
[[ "${HOME:-}" == /* ]] || die "HOME が絶対パスではありません"
BIN_DIR="$HOME/bin/swarm-console"
BIN="$BIN_DIR/swarm-console"
UNIT_DIR="$HOME/.config/systemd/user"
UNIT="$UNIT_DIR/${SERVICE}.service"
CONFIG="$HOME/.config/swarm-console/config.toml"
USER_NAME="${USER:-$(id -un)}"

WORK=""
TMP_BIN=""
TMP_UNIT=""
cleanup() {
  [[ -z "$WORK" ]] || rm -rf "$WORK"
  [[ -z "$TMP_BIN" ]] || rm -f "$TMP_BIN"
  [[ -z "$TMP_UNIT" ]] || rm -f "$TMP_UNIT"
}
trap cleanup EXIT

# --- 1. 前提の確認（何も置く前に） ---
[[ "$(uname -s)" == "Linux" && "$(uname -m)" == "x86_64" ]] || die "対応環境は Linux（linux/amd64）だけです（$(uname -s)/$(uname -m)）"
for cmd in curl sha256sum systemctl loginctl install cmp mktemp awk sort; do
  command -v "$cmd" >/dev/null 2>&1 || die "$cmd が見つかりません"
done
systemctl --user show-environment >/dev/null 2>&1 || die "systemctl --user が使えません（ユーザーの systemd に接続できない）"
[[ -f "$UNIT_SRC" ]] || die "$UNIT_SRC がありません（リポジトリの直下で make install を実行してください）"

# --- 2. 最新リリースの版を決める（/releases/latest の転送先 URL の末尾） ---
fetch() { # fetch <出力先> <URL> <最大バイト数>
  curl -fsSL --proto '=https' --proto-redir '=https' --connect-timeout 10 --max-time 300 \
    --max-filesize "$3" -o "$1" "$2"
}
final_url="$(curl -fsSL --proto '=https' --proto-redir '=https' --connect-timeout 10 --max-time 60 \
  -o /dev/null -w '%{url_effective}' "$RELEASES/latest")" || die "最新リリースを確認できません"
tag="${final_url##*/}"
[[ "$tag" =~ $TAG_RE ]] || die "最新リリースの版が vX.Y.Z の形ではありません: $tag"
asset="swarm-console_${tag}_linux_amd64"

# --- 3. ダウンロードと SHA256SUMS の照合（合わなければ何も置かずに失敗） ---
WORK="$(mktemp -d)"
fetch "$WORK/$asset" "$RELEASES/download/${tag}/${asset}" $((64 * 1024 * 1024)) || die "$asset をダウンロードできません"
fetch "$WORK/SHA256SUMS" "$RELEASES/download/${tag}/SHA256SUMS" $((64 * 1024)) || die "SHA256SUMS をダウンロードできません"

# 行末の CR と、ファイル名の直前の * は許す。名前が一致する行がちょうど 1 行で、ハッシュが 64 桁の小文字 16 進のとき比べる
want="$(awk -v name="$asset" '
  { sub(/\r$/, "") }
  NF == 2 && ($2 == name || $2 == ("*" name)) { n++; h = $1 }
  END { if (n == 1) print h }' "$WORK/SHA256SUMS")"
[[ "$want" =~ ^[0-9a-f]{64}$ ]] || die "SHA256SUMS に $asset の行がちょうど 1 行ありません（または形式が違います）"
got="$(sha256sum "$WORK/$asset")"
got="${got%% *}"
[[ "$got" == "$want" ]] || die "ハッシュが一致しません（期待 $want、実際 $got）。何も置いていません"

chmod 0755 "$WORK/$asset"
new_version="$("$WORK/$asset" version 2>/dev/null | awk 'NR == 1')" || true
[[ "$new_version" == "$tag" ]] || die "ダウンロードしたバイナリの版（${new_version:-取得できず}）がリリースの版 $tag と違います。何も置いていません"

# --- 4. バイナリを置く（本体の名前が消える瞬間を作らない：同じフォルダに書いて rename） ---
[[ ! -L "$BIN" ]] || die "$BIN がシンボリックリンクです。自己更新は通常ファイルだけを入れ替えるので、置き換えません。リンクを消してからやり直してください"
[[ ! -e "$BIN" || -f "$BIN" ]] || die "$BIN が通常ファイルではありません"
mkdir -p "$BIN_DIR"

place=yes
bin_status=installed
if [[ -e "$BIN" ]]; then
  cur="$(sha256sum "$BIN")"
  cur="${cur%% *}"
  cur_version="$("$BIN" version 2>/dev/null | awk 'NR == 1')" || true
  if [[ "$cur" == "$want" ]]; then
    place=no
    bin_status="unchanged（すでに最新の $tag）"
  elif [[ "$cur_version" =~ $TAG_RE && "$(printf '%s\n%s\n' "$cur_version" "$tag" | sort -V | tail -n 1)" == "$cur_version" ]]; then
    # 入っている版が最新リリース以上（ダウングレードしない。FR-UPD-12 と同じ）
    place=no
    bin_status="kept（入っている $cur_version は最新リリース $tag 以上なので置き換えない）"
  else
    bin_status="updated（${cur_version:-版を確認できず} → $tag）"
  fi
fi
service_was_active=no
systemctl --user is-active --quiet "$SERVICE" 2>/dev/null && service_was_active=yes
if [[ "$place" == yes ]]; then
  TMP_BIN="$(mktemp "$BIN_DIR/.swarm-console.install.XXXXXX")"
  install -m 0755 "$WORK/$asset" "$TMP_BIN"
  mv -f "$TMP_BIN" "$BIN"
  TMP_BIN=""
fi

# --- 5. unit を置く（実ファイルのコピー。同じ内容なら触らない） ---
mkdir -p "$UNIT_DIR"
unit_status="unchanged"
if ! cmp -s "$UNIT_SRC" "$UNIT"; then
  TMP_UNIT="$(mktemp "$UNIT_DIR/.${SERVICE}.service.XXXXXX")"
  install -m 0644 "$UNIT_SRC" "$TMP_UNIT"
  mv -f "$TMP_UNIT" "$UNIT"
  TMP_UNIT=""
  unit_status="written"
  systemctl --user daemon-reload
fi

# --- 6. 有効化と起動（設定が無いときは有効にするだけ。動いているサービスは再起動しない） ---
config_present=no
[[ -f "$CONFIG" ]] && config_present=yes
if [[ "$config_present" == yes ]]; then
  systemctl --user enable --now "$SERVICE"
else
  systemctl --user enable "$SERVICE"
fi

# --- 7. リンガリング（すでに yes なら何もしない） ---
linger="$(loginctl show-user "$USER_NAME" --property=Linger --value 2>/dev/null || true)"
if [[ "$linger" == "yes" ]]; then
  linger_status="unchanged（すでに yes）"
else
  loginctl enable-linger "$USER_NAME" || die "loginctl enable-linger に失敗しました。権限を確認してください"
  linger_status="enabled"
fi

# --- 8. 結果 ---
say "版: $tag"
say "バイナリ: $BIN_DIR/swarm-console … $bin_status"
say "unit: $UNIT … $unit_status"
say "リンガリング: $linger_status"
if [[ "$config_present" == no ]]; then
  say "設定ファイルがありません: $CONFIG"
  say "サービスは有効にしただけで、起動していません。次の順に進めてください："
  say "  1. ~/bin/swarm-console/swarm-console setup   （対話型。端末が要る。config.toml を手で書いてもよい）"
  say "  2. ~/bin/swarm-console/swarm-console token add <名前> --scope ops"
  say "  3. make install をもう一度打つ（または systemctl --user start $SERVICE）"
  exit 0
fi

# 起動を待つ（上限 10 秒）。tailscale が上がっていないなどで落ち続ければ失敗にする
active=no
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if systemctl --user is-active --quiet "$SERVICE"; then
    active=yes
    break
  fi
  sleep 1
done
[[ "$active" == yes ]] || die "サービスが active になりません。journalctl --user -u $SERVICE を見てください"
say "サービス: active"
if [[ "$place" == yes && "$service_was_active" == yes ]]; then
  say "動いていたサービスは再起動していません。新しい版にするには systemctl --user restart $SERVICE（serve の自己更新でも入れ替わります）"
fi
