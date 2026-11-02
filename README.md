# swarm-console

tmux の中で動く AI コーディングエージェント（omp・Claude Code・Codex）を、MCP 経由で見る・操作する常駐サーバーです。

- ペインの状態・質問カード・最終報告を、画面をまるごと読まずに短い JSON で取れます
- 質問カードへの回答・貼り付け・ペインの起動と終了などを、1 回の呼び出しで行えます
- ペインごとの引き継ぎメモと、閉じたペインの履歴を残せます
- 窓口は 2 つです。`/report`（見るだけ）と `/ops`（見る＋操作）。ポートは 48120 で、tailnet（Tailscale のネットワーク）の中だけで待ち受けます。インターネットには出しません

目的・スコープは [プロジェクト概要](docs/overview.md)、全体の作りは [アーキテクチャ設計](docs/architecture/overview.md) を参照してください。

## 対応環境

- Linux（linux/amd64）だけが対象です。**Windows と macOS は対象外**です
- tmux 3.x（`load-buffer -` と `paste-buffer -p` が使えること）
- systemd のユーザーサービス（常駐のために使います）
- tailnet に入っていること（`listen_addr` に tailnet の IP を書きます）

## インストール

リポジトリを取得して、`make install` を 1 回打ちます（`sudo` は要りません。`curl`・`sha256sum`・`systemctl`・`loginctl` が使えること）。

```sh
git clone https://github.com/ousiassllc/swarm-console.git
cd swarm-console
make install
```

`make install` は次を順に行います。手元ビルドは使わず、入れるのは常に [GitHub Releases](https://github.com/ousiassllc/swarm-console/releases) の最新リリースです。

1. 最新リリースの `swarm-console_vX.Y.Z_linux_amd64` と `SHA256SUMS` を取得し、ハッシュを照合します。**一致しなければ、何も置かずに失敗します。**
2. `~/bin/swarm-console/swarm-console` に通常ファイルとして置きます（シンボリックリンクにしません。自己更新の条件です。配置先がシンボリックリンクのときは、置き換えずに失敗します）。
3. `deploy/swarm-console.service` を `~/.config/systemd/user/` にコピーし、`systemctl --user daemon-reload` と `enable` をします。
4. `~/.config/swarm-console/config.toml` があれば `enable --now` で起動し、`active` になるまで確かめます。**無いときは、設定を作らず、起動せずに止まって次の手順を表示します**（終了コードは 0）。「設定」の節で `config.toml` とトークンを用意して、もう一度 `make install` を打ちます。
5. `loginctl enable-linger` で、ログインしていなくても動くようにします（ユーザーサービスはこれが無いと、ログアウトで止まります。すでに有効なら何もしません）。

何回打っても同じ結果になります。

- すでに最新のバイナリが入っていれば、バイナリにも unit にも触れません（`unchanged`）。
- 入っている版が最新リリース以上なら、置き換えません（`kept`。ダウングレードしません）。古いときだけ上書きします（`updated`）。
- 動いているサービスは再起動しません。新しい版にするには `systemctl --user restart swarm-console` です（`serve` の自己更新でも入れ替わります）。

実際のホームを使わずに動作を確かめるテストは `make install-test` です（偽の `curl`・`systemctl`・`loginctl` と一時ディレクトリの `HOME` を使います）。

状態は `systemctl --user status swarm-console` と `journalctl --user -u swarm-console`（ログは JSON Lines）で見ます。デプロイ構成の全体は [アーキテクチャ：デプロイ構成](docs/architecture/deployment.md#7-デプロイ構成) を参照してください。

## 設定

設定は `~/.config/swarm-console/` に置きます（`config.toml`・`profiles.toml`・`tokens.toml`）。`config.toml` は次のどちらで作ってもよく、`serve` が読む内容は同じです。

### 対話型（`setup`）

端末（TTY）が要ります。ファイルが無ければ新規作成し、あれば現在値を初期値に開いて編集できます。手で書いた値・コメントは、画面で変えたキー以外は 1 バイトも変わりません。最後に任意でトークンも発行できます。

```sh
~/bin/swarm-console/swarm-console setup                      # 既定は ~/.config/swarm-console/config.toml
~/bin/swarm-console/swarm-console setup --config ./my.toml   # 別の場所
```

端末でない環境（SSH の `-t` なし・CI・パイプ）では何もせず終了コード 1 で終わります。その場合は手書きにします。聞かれる項目は [機能要件 §11](docs/requirements/functional-note.md#11-対話型の設定fr-set) を参照してください。

### 手書き

`listen_addr` だけが必須です。tailnet の IP（`tailscale ip -4` の値）とポート 48120 を書きます。`0.0.0.0`・`::`・ホスト名は起動を拒否されます（テスト用にループバックは許可）。

```toml
listen_addr = "100.x.y.z:48120"   # tailscale ip -4 の値
sessions = ["0"]                  # 対象の tmux セッション
```

キーの一覧と既定値は [アーキテクチャ：設定ファイルの全体像](docs/architecture/deployment.md#8-設定ファイルの全体像)、型と検証は [ER：config.toml](docs/architecture/er-config.md#5-configtoml) にあります。`denied_paths` は書くと既定（`~/dotfiles`）と入れ替わる点に注意してください。

保存したら `systemctl --user restart swarm-console` で反映します（`setup` は再起動しません）。

### トークン

クライアントごとに別のトークンを発行します。窓口の範囲を `--scope` で指定します（`ops` か `report`）。平文は標準出力に 1 度しか表示されません。

```sh
~/bin/swarm-console/swarm-console token add my-pc --scope ops
~/bin/swarm-console/swarm-console token revoke my-pc        # 再起動なしで次のリクエストから効く
```

## レポートの使用状況・omp の版

`report` は、Claude・Cursor・Codex の使用状況、CPU・スワップ・ディスクの使用率、omp の新しい版の有無、ログイン期限を返します。`host_status` は、ログイン期限を除く値（使用状況・CPU・スワップ・ディスク・omp の版）と、swarm-console 自身の更新の状況（`update`）を返します。ログイン期限は `agent_auth_status`（と `report`）で取ります（[機能要件 FR-REP-10〜18](docs/requirements/functional-report.md)）。値はスナップショットを作るときにまとめて入れるので、同じスナップショットなら出力は同じです。

- `usage_check`（既定 `true`）・`usage_interval`（既定 `5m`、`1m` 以上）：`serve` が `omp usage --json` を実行して使用状況を取ります。`false` なら実行せず、レポートは「無効」と出します
- omp の更新確認（`omp update --check`）は、下の `update_check`・`update_check_interval` に従います
- `serve` が実行する外部コマンドは、この引数固定の 2 つだけです（`[agents.omp] command` の先頭の語の実行ファイル。標準入力は `/dev/null`、シェルなし、打ち切り付き。`omp update` は実行しません）
- メール・`accountId` は出力に出ません。アカウントは名前（メールの `@` 以降の最初のラベル）だけで示します。取れない値は消さずに「取得できず」と出します

## 更新

既定では、`serve` が自分で更新します（自己更新）。

- `update_check`（既定 `true`）：`serve` が 24 時間ごとに GitHub Releases の最新版を確認します（間隔は `update_check_interval`）
- `update_auto`（既定 `true`）：新しい版を見つけたら、ダウンロード → `SHA256SUMS` で検証 → 入れ替え → 正常終了し、systemd が新しいバイナリを起動します。新しいバイナリが起動に失敗し続けても、3 回目の起動で自動的に `.old` に戻して終了します
- 確認だけを手で行うときは `swarm-console version --check`（終了コード：最新 0、新しい版あり 10、確認できない 1）。状況は `host_status` の `update` でも見られます
- 検証は `SHA256SUMS` のハッシュだけです（署名なし）。限界は [機能要件 §12.5](docs/requirements/functional-update.md#125-リスクと限界) を参照してください

自己更新を止めるには、`config.toml` に `update_auto = false` を書いて再起動します。そのときの更新は、新しいバイナリを `~/bin/swarm-console/swarm-console` に置き、`systemctl --user restart swarm-console` します。

### `.old` から手で戻す

入れ替えのとき、旧バイナリは同じフォルダの `swarm-console.old` に 1 世代だけ残ります。手で前の版に戻すには、次のようにします。

```sh
mv ~/bin/swarm-console/swarm-console.old ~/bin/swarm-console/swarm-console
systemctl --user restart swarm-console
```

- `update_auto = false` にしておかないと、次の確認で再び最新版へ入れ替わります
- 再起動を繰り返して systemd が起動を諦めたときは、先に `systemctl --user reset-failed swarm-console` を実行します
- 自動で戻された版を再び試すときは、`~/bin/swarm-console/swarm-console.update-failed` を消します

詳細は [機能要件 §12](docs/requirements/functional-update.md#12-新しい版の確認と自己更新fr-upd) と [§12.6 運用者の操作](docs/requirements/functional-update.md#126-運用者の操作) を参照してください。

## MCP クライアントからのつなぎ方

MCP（Streamable HTTP）の URL とトークンをクライアントに設定します。トークンは `Authorization: Bearer <トークン>` で送ります。

| 窓口 | URL | できること | 使えるトークン |
|------|-----|-----------|---------------|
| `/report` | `http://<tailnet-ip>:48120/report` | 見るだけ（9 ツール：`report`・`panes_status`・`pane_tail`・`ask_card`・`report_read`・`host_status`・`agent_auth_status`・`note_get`・`note_history`） | `report` と `ops` のどちらでも |
| `/ops` | `http://<tailnet-ip>:48120/ops` | 見る＋操作（上の 9 に、`ask_pick`・`send_text`・`send_escape`・`pane_start`・`pane_close`・`dispatch`・`note_set`・`note_log` の 8 を足した 17 ツール） | `ops` だけ |

- 定期レポートのように見るだけでよいクライアントは、`report` のトークンで `/report` につなぎます。`report` のトークンで `/ops` を呼ぶと 403（`forbidden_scope`）です
- `POST` のみ受け付けます（ステートレス運用）。トークンなし・不一致は 401 です
- tailnet の外からはつながりません。ユーザースペースの tailscale を使うクライアントは、SOCKS5 経由で呼びます

curl での呼び出し例（トークンは環境変数 `SWARM_CONSOLE_TOKEN` から読み、ログに出さない）：

```sh
curl --socks5-hostname localhost:<port> -sS "http://<tailnet-ip>:48120/ops" \
  -H "Authorization: Bearer $SWARM_CONSOLE_TOKEN" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"panes_status","arguments":{}}}'
```

（tailscale に直接つながる環境では `--socks5-hostname localhost:<port>` は要りません。）窓口・認証・エラー・全ツールの入出力は [API 仕様](docs/api/endpoints.md) を参照してください。

## 開発者向け

- 必要なツールと初回セットアップ（Go 1.26 以上・tmux・golangci-lint・Lefthook・Linterly・make）は [環境構築](docs/environment/setup.md) を参照してください
- ビルドとチェックは `make` で行います。`make check` は CI（`.github/workflows/ci.yml`）と同じ順序・同じコマンドで、`build`・`vet`・`lint`・`linterly`・`plan-test`・`install-test`・`test`（`go test -race ./...`）を順に実行します。`install-test` は `scripts/install_test.sh` で、偽の `curl`・`systemctl`・`loginctl` と一時 `HOME` を使うので、実際の GitHub・systemd・`HOME` には触れません。lint のキャッシュは worktree ごと（`.cache/golangci-lint`）に分かれ、隣の worktree のファイルは混ざりません。`golangci-lint` を直接打つときは `GOLANGCI_LINT_CACHE="$PWD/.cache/golangci-lint"` を付けてください。詳細は [環境構築：Lint・Format・Linterly・Git Hooks・Makefile](docs/environment/setup-quality.md#タスクランナーmakefile) を参照してください
- Git Hooks は `lefthook install` で有効にします（pre-commit と pre-push。`--no-verify` は使いません）
- テストは実際の tmux を使います。専用ソケットで動かすので、動いている tmux には触れません
- CI は GitHub Actions です。`main` の CI が成功すると、`release.yml` が次の版（パッチ +1）のタグを付け、`swarm-console_<タグ>_linux_amd64` と `SHA256SUMS` を GitHub Releases に載せます。マイナー・メジャーを上げるときは手で `vX.Y.Z` のタグを打ちます。詳細は [環境構築：CI/CD](docs/environment/setup-ci.md#cicd) を参照してください
- ソースの構成は [コンポーネント設計](docs/components/overview.md)、仕様書の一覧は [プロジェクト概要のドキュメントマップ](docs/overview.md#ドキュメントマップ) にあります

```sh
make build        # bin/swarm-console を作る
make test         # go test -race ./...
make check        # CI と同じ順序で全部
```
