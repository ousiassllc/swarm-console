# プロジェクト概要

## 目的

tmux の中で動く AI コーディングエージェント（omp・Claude Code・Codex）を、MCP 経由で次の 4 点が満たせる形にする常駐サーバー `swarm-console` を作る。

1. ペインの画面をまるごと読まずに、状態・質問カード・最終報告を短い JSON で取れる（文字量を減らす）
2. 貼り付け・選択・閉じるなどの手順を 1 回の呼び出しで正しく行える（SSH の回数と手順ミスを減らす）
3. ペインごとに「誰の依頼で、どこに報告し、次に何をするか」をメモとして残し、閉じたあとも履歴で探せる
4. omp だけでなく Claude Code と Codex も同じツールで扱える

## 背景

- example-host 上の tmux に複数のエージェントペインを並べて作業させており、運用エージェントがクライアント環境から tailnet 越しに SSH で画面を読み、キーを送って操作している。
- 画面全体を読むと文字量が大きく、貼り付け・選択・閉じるの手順を SSH 越しに毎回手で組み立てるため、手順ミスと呼び出し回数が増えている。
- ペインを引き継ぐたびに会話を遡って「依頼元・報告先・次にやること」を探している。
- 利用クライアントは curl に限らず、tailnet 内のパソコンの Claude Code・Codex・Cursor など MCP をしゃべれるものすべて。クライアントごとに表示や指示文が揺れないよう、見た目と指示文はサーバー側で組み立てる。
- 元仕様は「swarm-console 仕様 v1.1（完全版）」。本ドキュメント群はそれを仕様書として分割・具体化したもの。

## スコープ

### 含むもの

- MCP（Streamable HTTP）サーバー。窓口は `/report`（見るだけ）と `/ops`（見る＋操作）の 2 つ、ポート 48120（tailnet IP のみで待ち受け）
- 3 つのエージェント（omp・Claude Code・Codex）のアダプタ。画面から状態・質問カード・活動を判定する
- ツール 18 個：見る 10（`report`・`sessions_list`・`panes_status`・`pane_tail`・`ask_card`・`report_read`・`host_status`・`agent_auth_status`・`note_get`・`note_history`）と操作 8（`ask_pick`・`send_text`・`send_escape`・`pane_start`・`pane_close`・`dispatch`・`note_set`・`note_log`）
- ペインごとのメモと閉じたペインの履歴（JSON ファイル）
- サーバー側で組み立てる `report`（日本語・英語の markdown）と `dispatch`（条件のひな形つき指示文）
- MCP の prompts（`report`・`dispatch`）と resources（`swarm://report`・`swarm://pane/<位置>`・`swarm://notes/<位置>`）
- クライアントごとの Bearer トークン（窓口付き。触れる tmux セッションも付けられる）の発行・無効化 CLI
- systemd ユーザーサービスとしての常駐、GitHub Releases での配布、CI（build・vet・golangci-lint・linterly・release plan test・install script test・test -race。[環境構築：CI/CD](environment/setup-ci.md#cicd)）
- 実 tmux と擬似エージェント画面を使ったテスト

### 含まないもの

- 任意のシェルコマンドの実行
- tmux セッション・ペインの作成・削除・分割（今あるペインを見つけて使う）
- コードのレビューや git 操作（エージェント側の仕事）
- インターネットへの公開（Funnel・Tunnel は使わない）
- ツールごと・クライアントごとの許可設定（`send_escape` の利用可否はクライアント単位では持たない。トークンに付けられるのは触れる tmux セッションだけ）
- クライアント側の呼び出しスクリプト `sc.sh`（リポジトリ外。本サーバーの利用側）
- 段階リリース（§9 のとおり全機能を 1 本で実装する）

## ステークホルダー / 想定ユーザー

| 区分 | 役割 | 備考 |
|------|------|------|
| オーナー | 依頼元・承認者。`send_escape` の OK を出す | 実装着手の最終 OK もオーナー |
| 運用エージェント | 主な利用者。クライアント環境から tailnet 越しに `/ops` を呼び、ペインの監視・指示・引き継ぎを行う | クライアント環境の curl（`sc.sh`） |
| agent-b・agent-a ほか依頼元エージェント | メモの `from`・`report_to` に現れる依頼元・報告先 | 本サーバーを直接呼ぶとは限らない |
| パソコンの AI ツール（Claude Code・Codex・Cursor など） | MCP クライアント。レポート表示や操作を行う | クライアントを決め打ちしない |
| ルーティン（定期レポート） | `/report` のみに接続して `report` を取る | `report` スコープのトークン |
| 運用者（オーナー兼任） | トークン発行、`update_auto = false` のときのバイナリ入れ替えと `systemctl --user restart`（既定は `serve` が自己更新する。機能要件 §12.4） | example-host 上で実施 |

## システム全体像

```mermaid
graph TD
    subgraph Clients["MCP クライアント（tailnet 内）"]
        CLIENT["クライアント環境: sc.sh (curl, SOCKS5 経由)"]
        PC["パソコン: Claude Code / Codex / Cursor ほか"]
        ROUTINE["ルーティン（レポートのみ）"]
    end

    subgraph Desktop["example-host"]
        subgraph SC["swarm-console（systemd user, tailnet IP:48120）"]
            REP["/report 窓口（見るだけ）"]
            OPS["/ops 窓口（見る＋操作）"]
            CORE["ペイン台帳・スナップショット・アダプタ・メモ・レンダラ"]
        end
        TMUX["tmux サーバー（セッション 0 ほか）"]
        AG["omp / claude / codex（各ペイン）"]
        FS["~/.config/swarm-console, ~/.local/share/swarm-console, /tmp/swarm-console/reports"]
    end

    CLIENT -->|tailscale / Bearer| OPS
    PC -->|tailscale / Bearer| OPS
    PC -->|tailscale / Bearer| REP
    ROUTINE -->|tailscale / Bearer| REP
    REP --> CORE
    OPS --> CORE
    CORE -->|capture-pane / send-keys / paste-buffer| TMUX
    TMUX --- AG
    CORE --> FS
```

アーキテクチャの詳細は [architecture/overview.md](architecture/overview.md) を参照。

### 2 セッション運用

tmux セッションを分けて（例：セッション `0` は agent-a、`1` は agent-b。どちらも 3 列×2 段で `T1`〜`T3`・`B1`〜`B3`）運用できる。トークンごとに触れるセッション（`allowed_sessions`。`token add --sessions 0`）を決め、ツールの任意引数 `session`（許可が 1 つなら省略可、2 つ以上なら必須）でセッションを選ぶ。ペインはセッションの中で解決するので、別のセッションの `T1` と取り違えず、許可外のセッションのペインは読めも操作もできない（`session_forbidden`）。`sessions_list` で自分が触れるセッションとそのレイアウトを取れる。`sessions = ["0"]` だけの運用は今までどおり引数なしで動く。詳細は [機能要件 FR-SESS](requirements/functional.md#21-セッションの許可と解決fr-sess)・[API 仕様 §4.1](api/endpoints.md#41-session-引数と許可セッション)。

## ドキュメントマップ

このプロジェクトの仕様は以下のドキュメントで構成される。実装時はそれぞれ参照すること。仕様書も 1 ファイル 300 行以内（Linterly の上限）にするため、長い文書は責務ごとに複数のファイルへ分けてある（節番号は通し）。

| ドキュメント | パス | 概要 |
|------------|------|------|
| 機能要件 | `docs/requirements/functional.md` | ユースケース、ペイン位置、セッションの許可と解決（FR-SESS）、状態遷移、機能一覧（18 ツール） |
| 機能要件：レポート・質問カード・指示 | `docs/requirements/functional-report.md` | レポート、質問カードの操作、指示文の組み立て、受け入れ確認 |
| 機能要件：メモ・安全・設定 | `docs/requirements/functional-note.md` | メモ、安全のための決まり、対話型の設定（`setup`） |
| 機能要件：新しい版の確認と自己更新 | `docs/requirements/functional-update.md` | 新しい版の確認（`version --check`）と自己更新 |
| 機能要件：インストール | `docs/requirements/functional-install.md` | `make install`（最新リリースの取得・照合・配置・systemd 常駐。何回流しても同じ結果） |
| 非機能要件 | `docs/requirements/non-functional.md` | 性能目標、セキュリティ（tailnet 限定・認証・拒否規則）、可用性、運用、テスト・CI 要件 |
| アーキテクチャ設計 | `docs/architecture/overview.md` | 全体構成、技術選定、レイヤー構造、スナップショット方式、通信フロー、認証と窓口 |
| アーキテクチャ：デプロイ構成・設定ファイル | `docs/architecture/deployment.md` | デプロイ構成、設定ファイルの全体像 |
| アーキテクチャ：提案した既定値 | `docs/architecture/decisions.md` | 提案した既定値と実装時確認事項（P-xx）の一覧 |
| エージェントの画面判定 | `docs/architecture/agent-patterns.md` | 状態判定・質問カードの正規表現（P-22） |
| エージェントの認証期限 | `docs/architecture/agent-auth.md` | `agent_auth_status` の読み先と判定表（P-16） |
| レポートに足す値の集め方 | `docs/architecture/report-values.md` | 使用状況・CPU・スワップ・ディスク・omp の版・ログイン期限の取り方、間隔、取れないときの値（P-60〜P-67） |
| ER / データモデル | `docs/architecture/er.md` | ファイル一覧、全体 ER 図、notes.json |
| ER：tokens.toml・実行時のペイン状態 | `docs/architecture/er-runtime.md` | tokens.toml、実行時のペイン状態（メモリ） |
| ER：config.toml・profiles.toml ほか | `docs/architecture/er-config.md` | config.toml、profiles.toml、最終報告ファイル、自己更新のファイル |
| API 仕様 | `docs/api/endpoints.md` | 窓口・認証・エラー形式・共通の決まり |
| API 仕様：ペインを見る・操作する | `docs/api/endpoints-pane.md` | `panes_status`・`pane_tail`・`ask_card`・`report_read`・`ask_pick`・`send_text`・`send_escape`・`pane_start`・`pane_close`（`session` 引数付き） |
| API 仕様：サーバーの状態・メモ | `docs/api/endpoints-state.md` | `sessions_list`・`host_status`・`agent_auth_status`・`note_*` |
| API 仕様：レポート・指示・prompts・上限 | `docs/api/endpoints-report.md` | `report`・`dispatch`・prompts・resources・上限一覧 |
| コンポーネント設計 | `docs/components/overview.md` | Go パッケージ構成、アダプタ IF、責務、依存関係、テスト構成 |
| 環境構築 | `docs/environment/setup.md` | 概要、ディレクトリ構造、開発環境セットアップ |
| 環境構築：CI/CD | `docs/environment/setup-ci.md` | CI/CD（`ci.yml`・`release.yml`・自動タグ付きリリース・デプロイ） |
| 環境構築：Lint・Format・Linterly・Git Hooks・Makefile | `docs/environment/setup-quality.md` | golangci-lint、Linterly、Lefthook、Makefile（Swagger は対象外） |

## マイルストーン / リリース計画

段階リリースはしない。全機能を 1 本で実装する（元仕様 §9）。

- M1: §5〜§7 の全機能（3 アダプタ、見る・操作・メモ・サーバー状態・レポート・指示）を実装。完了条件は、テストが全部通り、CI が緑で、受け入れ確認（機能要件 §8）が通ること
- 実装着手はオーナーの OK のあと

## 用語集

| 用語 | 定義 |
|------|------|
| ペイン | tmux のペイン。1 ペインに 1 エージェント（またはシェル）が入る |
| 位置 | ペインの座標から自動で付ける名前（`T1`・`B4`、既定セッション以外の全体の名前は `1:T1`・`work:B2`）。1 つのセッションに絞った応答では、そのセッション内の位置（接頭辞なし）で返す。tmux ID（`%12`）でも指定できる |
| アダプタ | エージェントごとの違い（起動・終了・状態判定・質問カード・キー操作・飾りの除去）を閉じ込めたモジュール |
| 窓口 | 同じポート 48120 のパスで分けた入口。`/report`（見るだけ）と `/ops`（全部） |
| スコープ | トークンに付く使える窓口（`report` または `ops`） |
| 許可セッション | トークンに付く、触れる tmux セッション（`allowed_sessions`）。`config.toml` の `sessions` との共通部分。省略は `sessions` 全部。許可外のセッションのペインは読めも操作もできない |
| 質問カード | エージェントが選択肢つきで止まっている画面（omp の Ask カード、Claude Code の確認、Codex の承認） |
| 状態 | `working`・`asking`・`idle`・`shell`・`stalled`・`unknown` の 6 つ |
| メモ | ペインごとの引き継ぎ情報（依頼元・報告先・リポジトリ・次にやること など） |
| 履歴 | `pane_close` で閉じたペインのメモの保管（最大 500 件） |
| 最終報告 | エージェントが `/tmp/swarm-console/reports/<位置>.md` に書くファイル。`report_read` で読む |
| スナップショット | 全ペインの画面・状態・ホスト情報を一度に取った結果。`report` と `panes_status` はこれから作る（アーキテクチャ §4） |
| プロファイル | `dispatch` で指示文に足す条件のひな形（`sweep`・`fix`・`investigate`）。`profiles.toml` に置く |
| dispatch | 中身（task）と条件（profile）から決まった形の指示文を組み立てて送るツール |
| tailnet | Tailscale のネットワーク。本サーバーはこの中だけで待ち受ける |
| クライアント環境 | 運用エージェントが動く環境。ユーザースペース tailscale（SOCKS5 localhost:<port>）から接続する |
| omp | 本リポジトリの対象エージェントの 1 つ（`omp` コマンド） |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-31 | 新規作成（元仕様 v1.1 を仕様書に展開） | 初版 |
| 1.1 | 2026-10-31 | ドキュメントマップに環境構築を追加 | 環境構築ドキュメントの作成に伴い |
| 1.2 | 2026-11-01 | 運用者の役割を自己更新に合わせて更新 | Issue #7（自己更新で確定） |
| 1.3 | 2026-11-01 | 300 行超過ファイルを分割し、ドキュメントマップを新しいファイルに合わせて更新。内容の変更なし | Issue #3 |
| 1.4 | 2026-11-01 | ドキュメントマップに機能要件：インストール（`make install`）を追加 | Issue #18 |
| 1.5 | 2026-11-02 | スコープの CI の説明に linterly・release plan test・install script test を追加（`ci.yml` に合わせた） | Issue #41 |
| 1.6 | 2026-11-02 | ツール数 17→18（`sessions_list`）、トークンごとの許可セッション・2 セッション運用の概要・用語（許可セッション、位置）・ドキュメントマップを追加・更新 | セッション分割運用（許可セッションの分離） |
