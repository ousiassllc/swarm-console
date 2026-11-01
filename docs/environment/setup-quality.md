# 環境構築：Lint・Format・Linterly・Git Hooks・Makefile

[環境構築](setup.md) の続き。Lint、Format、Linterly、Git Hooks、タスクランナー（Makefile）、Swagger/OpenAPI（対象外）を定める。

改訂履歴は [環境構築（本書）](setup.md#改訂履歴) にまとめてある。

## Lint

| 項目 | 内容 |
|------|------|
| ツール | golangci-lint v2（`version: "2"` 形式の設定） |
| 設定ファイル | `.golangci.yml` |
| 実行 | `make lint`（`golangci-lint run ./...`） |
| 方針 | standard セットに、安全要件（シェル実行なし・パス検証・制御文字）と相性のよい linter を足す。除外（`//nolint`）は理由コメント必須（`nolintlint`） |

```yaml
version: "2"
run:
  concurrency: 4        # 高負荷ホストで他の作業を圧迫しない
  timeout: 5m
linters:
  default: standard     # errcheck・govet・ineffassign・staticcheck・unused
  enable:
    - gosec             # 安全要件（シェル実行なし、パス・ファイル権限）
    - revive
    - errorlint         # errors.Is / As の徹底
    - bodyclose         # HTTP 応答の閉じ忘れ
    - noctx             # context なしの HTTP / exec
    - nolintlint
    - misspell
formatters:
  enable:
    - gofmt
    - goimports
```

- `gosec` は `os/exec` を使う `internal/tmux`・`internal/host` に対して指摘が出る。引数配列渡し・許可リストで安全が担保される箇所は `//nolint:gosec // 理由` を付ける（理由なしは `nolintlint` が拒否）。
- linter の追加・除外を変えるときは、このドキュメントと `.golangci.yml` を同じコミットで更新する。

## Format

| 項目 | 内容 |
|------|------|
| ツール | gofmt・goimports（golangci-lint v2 の `formatters`） |
| 設定ファイル | `.golangci.yml`（上記 `formatters`） |
| 実行 | `make fmt`（`golangci-lint fmt ./...`）、検査のみは `make fmt-check`（`golangci-lint fmt --diff ./...`） |
| 対象 | `*.go`。markdown・yml・toml は自動整形しない |

## Linterly

| 項目 | 内容 |
|------|------|
| 目的 | ファイル・ディレクトリの行数肥大化を防ぐ |
| インストール | `go install github.com/ousiassllc/linterly/cmd/linterly@latest`（Go プロジェクトのため bun ではなく go install） |
| 設定ファイル | `.linterly.yml`、除外は `.linterlyignore` |
| 実行 | `make linterly`（`linterly check`） |

```yaml
rules:
  # max_lines_per_file / max_lines_per_directory はデフォルト値（300/2000）を使う。
  # 特別な理由がない限り変更しない。
  # max_lines_per_file: 300
  # max_lines_per_directory: 2000
  warning_threshold: 10       # 早めに警告を出す

count_mode: all               # 変更しない
default_excludes: true
language: ja

# ignore でパス別に除外する場合は慎重に。生成コードなど明確な理由がある場合のみ追加する。
# ignore:
#   - "internal/generated/**"
```

`.linterlyignore`（今は何も除外しない。手書きのソースコードと仕様書（`docs/`）は除外しない）：

```
# 自動生成コード（必要になった場合のみ追加する）
# *.generated.go
# *.pb.go
```

- 上限（`warn`: 閾値超え・終了コード 0、`error`: 上限超え・終了コード 1）に当たったら、除外ではなくパッケージ・ファイルの分割で解消する。
- 仕様書（`docs/`）も同じ上限（300 行）の対象にする。上限に当たったら、`.linterlyignore` で除外せずファイルを責務ごとに分割する（[アーキテクチャ設計 P-31](../architecture/decisions.md#9-提案した既定値と実装時確認事項)）。
- テストの画面サンプル（`testdata/screens/`）は 1 ファイル 1 画面にして上限を守る。

## Git Hooks

| 項目 | 内容 |
|------|------|
| ツール | Lefthook（`lefthook.yml`） |
| 有効化 | `lefthook install`（初回セットアップ） |
| 方針 | `--no-verify` は使わない。フックが失敗したらスキップせず原因を直す |

```yaml
pre-commit:
  parallel: false
  commands:
    format:
      glob: "*.go"
      run: golangci-lint fmt {staged_files}
      stage_fixed: true
    lint:
      glob: "*.go"
      run: golangci-lint run --new-from-rev=HEAD ./...
    linterly:
      run: linterly check

pre-push:
  parallel: false
  commands:
    build:
      run: go build ./...
    vet:
      run: go vet ./...
    test:
      run: go test -race ./...
```

| フック | 内容 | 狙い |
|--------|------|------|
| pre-commit | gofmt・goimports（staged、整形結果を自動で add）→ golangci-lint（変更分）→ linterly | 小さく速い検査でコミットを汚さない |
| pre-push | `go build` → `go vet` → `go test -race`（別コマンドを直列で実行） | CI と同じコマンドを push 前にローカルで通す |

- pre-push の `go test -race` は tmux を使う統合テストを含み、高負荷ホストでは重い。push 前に `uptime` を確認し、load average が `nproc` を超える間は並列数を下げて実行する（上記「高負荷ホストでの実行」）。
- CI でしか確認できない項目（実クライアント接続など）は、報告で「未確認」と明記する。

## タスクランナー（Makefile）

| ターゲット | 内容 |
|-----------|------|
| `make build` | `go build -o bin/swarm-console ./cmd/swarm-console` |
| `make vet` | `go vet ./...` |
| `make lint` | `golangci-lint run ./...` |
| `make fmt` / `make fmt-check` | 整形 / 整形差分の検査 |
| `make linterly` | `linterly check` |
| `make plan-test` | `bash .github/scripts/plan-release_test.sh`（次の版を決める処理のテスト。NFR-TEST-15） |
| `make test` | `go test -race ./...` |
| `make check` | build → vet → lint → linterly → plan-test → test を別ターゲットとして順に実行 |

`make check` は CI の `ci.yml` と同じ順序・同じコマンドにする（差が出たら両方を直す）。

## Swagger/OpenAPI

対象外。swarm-console の API は MCP（Streamable HTTP、JSON-RPC。窓口は `/report` と `/ops`）で、REST のエンドポイント群ではない。入出力・エラー・ツールは [API 仕様](../api/endpoints.md) を正とし、`SWAGGER_ENABLED`・`/swagger`・Stoplight Elements は導入しない。
