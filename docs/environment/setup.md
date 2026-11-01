# 環境構築

swarm-console の開発環境・CI/CD・Lint・Format・Linterly・Git Hooks の構成を定める。要件は [非機能要件 §7](../requirements/non-functional.md#7-テストci-要件)、パッケージ構成は [コンポーネント設計 §2](../components/overview.md#2-パッケージ構成) を参照する。

この文書は 1 ファイル 300 行の上限（Linterly）のため、次のファイルに分けている。節番号は分割前の通し番号のまま、どのファイルでも同じ番号で引く。

- [環境構築（本書）](setup.md)：概要、ディレクトリ構造、開発環境セットアップ（ローカルで動かす）
- [環境構築：CI/CD](setup-ci.md)：CI/CD（`ci.yml`・`release.yml`）
- [環境構築：Lint・Format・Linterly・Git Hooks・Makefile](setup-quality.md)：Lint、Format、Linterly、Git Hooks、Makefile、Swagger/OpenAPI

## 概要

| 項目 | 内容 |
|------|------|
| 言語 | Go 1 バイナリ（`go.mod` の `go` ディレクティブは 1.26 以上。開発機は go1.26.5） |
| MCP | 公式 Go SDK（`github.com/modelcontextprotocol/go-sdk`） |
| パッケージ管理 | Go modules（`go.mod`・`go.sum`）。TypeScript / JavaScript は使わないため bun は不要 |
| 外部ランタイム依存 | tmux 3.x（開発機は 3.2a）。テストでも実 tmux を使う |
| CI/CD | GitHub Actions（`ci.yml`・`release.yml`）。GitHub ホストのランナー（`runs-on: ubuntu-latest`）で動かす。main の CI が成功すると `release.yml` が次の版のタグを付けて GitHub Releases に載せる（人がタグを打たなくてよい） |
| Lint / Format | golangci-lint v2（開発機は 2.12.2）。format も同じ設定で管理（gofmt・goimports） |
| 行数管理 | Linterly（`.linterly.yml`） |
| Git Hooks | Lefthook（開発機は 2.1.17） |
| タスクランナー | Makefile |
| Swagger / OpenAPI | 対象外（API は MCP（JSON-RPC）で、仕様は [API 仕様](../api/endpoints.md) で管理する） |

## ディレクトリ構造

パッケージ構成は [コンポーネント設計 §2](../components/overview.md#2-パッケージ構成) に定める。環境構築に関わるファイルは次のとおり。

```
swarm-console/
├── .github/workflows/ci.yml        CI
├── .github/workflows/release.yml   main の CI 成功（または手で打ったタグ v*）でタグを付け、バイナリを GitHub Releases へ
├── .golangci.yml                   lint / format 設定
├── .linterly.yml                   行数管理設定
├── .linterlyignore                 行数管理の除外（自動生成コードが出たときだけ書く。今は何も除外しない。P-31）
├── lefthook.yml                    Git Hooks
├── Makefile                        タスクランナー（`make install` で運用ホストに導入）
├── scripts/                        install.sh（`make install`）とそのテスト install_test.sh
├── go.mod / go.sum
└── docs/environment/               このドキュメント（setup.md・setup-ci.md・setup-quality.md）
```

## 開発環境セットアップ

### 必要なツール

| ツール | バージョン | 用途 | 導入 |
|--------|-----------|------|------|
| Go | 1.26 以上 | ビルド・テスト | https://go.dev/dl/ |
| tmux | 3.x | 統合テスト・実運用（`load-buffer -`・`paste-buffer -p` が必要） | `apt install tmux` |
| golangci-lint | v2 系（2.12 以上） | lint / format | 公式のインストールスクリプト、または `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest` |
| Lefthook | 2.x | Git Hooks | `go install github.com/evilmartians/lefthook@latest` |
| Linterly | 最新 | 行数管理 | `go install github.com/ousiassllc/linterly/cmd/linterly@latest` |
| make | — | タスクランナー | `apt install make` |
| tailscale | — | 実機での接続確認のみ（開発・テストでは不要） | 運用ホストに導入済み |

### 初回セットアップ

```sh
git clone git@github.com:ousiassllc/swarm-console.git
cd swarm-console
go mod download
lefthook install        # pre-commit / pre-push を有効化
make check              # build → vet → lint → linterly → test を順に実行
```

### ローカルで動かす

`listen_addr` はテスト用にループバックを許可している（[アーキテクチャ設計 P-01](../architecture/decisions.md#9-提案した既定値と実装時確認事項)）。実機では `tailscale ip -4` の値を書く。

```sh
mkdir -p ~/.config/swarm-console
cat > ~/.config/swarm-console/config.toml <<'EOF'
listen_addr = "127.0.0.1:48120"
sessions = ["0"]
# 作業フォルダにしてはいけないパス（cwd・file を拒否）。書かなければ既定の ["~/dotfiles"]。
# 書いたら既定と入れ替わるので、~/dotfiles も守るなら一緒に書く（~ は展開して絶対パスで比較）。
# denied_paths = ["~/dotfiles", "/mnt/nvme/ousiassllc/some-checkout"]
EOF
go run ./cmd/swarm-console token add dev --scope ops   # 表示されたトークンを控える（再表示不可）
go run ./cmd/swarm-console serve --config ~/.config/swarm-console/config.toml
```

手書きの代わりに、対話型でも同じ `config.toml` を作れる（[機能要件 §11](../requirements/functional-note.md#11-対話型の設定fr-set)）。端末（TTY）が要る。手で書いた `config.toml` を開いて直してもよく、触っていない項目は 1 バイトも変わらない。

```sh
go run ./cmd/swarm-console setup                       # 既定は ~/.config/swarm-console/config.toml
go run ./cmd/swarm-console setup --config ./my.toml    # 別の場所
```

端末でない環境（SSH の `-t` なし・CI・パイプ）では終了コード 1 で終わる。その場合は上のように手で書く。保存したら `systemctl --user restart swarm-console` で反映する。

### 運用ホストに入れる（`make install`）

開発ではなく、運用ホストへ最初に導入するときの 1 コマンド（要件は [機能要件 §13](../requirements/functional-install.md#13-インストールfr-ins)）。リポジトリの直下で打つ。GitHub Releases の最新リリースを取り、`SHA256SUMS` で照合してから `~/bin/swarm-console/swarm-console` に置き、`deploy/swarm-console.service` を `~/.config/systemd/user/` に置いて `enable`、リンガリングを有効にする。手元ビルドは使わない。

```sh
make install
```

- `~/.config/swarm-console/config.toml` があれば起動して `active` まで確かめる。無ければ起動せず、`setup` → `token add` → `make install` のやり直しの手順を表示する（設定は作らない）。
- 何回流しても同じ結果になる。同じ版が入っていれば何も変えず、入っている版が最新以上なら置き換えない。動いているサービスは再起動しない。
- ハッシュが合わなければ、何も置かずに失敗する。
- 実際のホームに触れずに試すには、`HOME` を一時ディレクトリにして実行する（`systemctl --user` は実際のユーザーの systemd に届くので、試すときは偽の `systemctl`・`loginctl` を PATH の先頭に置く。`scripts/install_test.sh` がその形でテストしている）。

### 高負荷ホストでの実行

開発機（example-host）は他の作業で高負荷になりやすい（CPU 数に対し load average が大きく上回る状態が続くことがある）。

- build・vet・lint・test は 1 本のコマンドに詰めず、別々に 1 つずつ実行する（`make check` も直列）。
- 実行前に `uptime` で load average を `nproc` と比べる。超えているときは並列数を下げる（`GOMAXPROCS`、`go test -p 4`、golangci-lint は `.golangci.yml` の `run.concurrency: 4`）。
- tmux を使うテストは専用ソケット（`-L`）を使い、動いている本番の tmux サーバー（セッション `0`）に触れない。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-31 | 新規作成 | 初版 |
| 1.1 | 2026-11-01 | 「ローカルで動かす」に `setup`（対話型）の使い方を追加 | Issue #5 |
| 1.2 | 2026-11-01 | release.yml の骨子の成果物名を `swarm-console_vX.Y.Z_linux_amd64` に、デプロイの記述を自己更新に更新 | Issue #7（自己更新で確定） |
| 1.3 | 2026-11-01 | CI/CD に自動タグ付きリリース（`release.yml` の `plan`・`verify`・`build`・`release`、版の上げ方、手で打ったタグの検査）を追加 | Issue #8 |
| 1.4 | 2026-11-01 | 成果物名を `swarm-console_<タグ>_linux_amd64`（版・OS・arch 入り）と `SHA256SUMS` の書式に変更、デプロイの記述を自己更新（Issue #7）に合わせて更新 | Issue #8（Issue #7 の自己更新で確定） |
| 1.5 | 2026-11-01 | CI に `plan-release_test.sh` のステップ、Makefile に `plan-test` を追加（NFR-TEST-15 を CI で動かす） | Issue #8 の実装 |
| 1.6 | 2026-11-01 | 300 行超過ファイルを分割（§CI/CD を setup-ci.md、Lint 以降を setup-quality.md へ移動。`.linterlyignore` の `docs/**` 除外を廃止（P-31）、ほかの内容は変更なし） | Issue #3 |
| 1.7 | 2026-11-01 | 「運用ホストに入れる（`make install`）」を追加、ディレクトリ構造に `scripts/`、Makefile に `install`・`install-test` を追加 | Issue #18 |
