# コンポーネント設計 改訂履歴

[コンポーネント設計](overview.md) の改訂履歴。

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-31 | 新規作成 | 初版 |
| 1.1 | 2026-11-01 | `setup`・`config/tomledit` を追加、`config` の公開 IF を拡張、テスト構成に `setup` を追加 | Issue #5 |
| 1.2 | 2026-11-01 | `updatecheck` を追加、`version --check` を追加 | Issue #7 |
| 1.3 | 2026-11-01 | `selfupdate` を追加（自己更新の確定。FR-UPD-11〜20）、テスト構成に自己更新を追加 | Issue #7（自己更新で確定） |
| 1.4 | 2026-11-01 | §2 の `.github/`、§7 の `release.yml` を自動タグ付きに更新 | Issue #8 |
| 1.5 | 2026-11-01 | `reportvalues` を追加（レポートに足す値の収集。FR-REP-10〜18） | Issue #21〜#25 |
| 1.6 | 2026-11-01 | §2 に `scripts/`（`make install` の `install.sh` とそのテスト）を追加 | Issue #18 |
| 1.7 | 2026-11-02 | `reportvalues` の入出力を実装に合わせた（`Runner`・`Start`、`pane.Values`）。`tools` から `UpdateSource` を除き、`host_status.update` もスナップショットの値を返す | Issue #21〜#25（実装時の確認） |
| 1.8 | 2026-11-02 | §5.2 `Close(ref)` に、終了コマンドのあと `shell` に戻るまで待つ手順（上限で `apperr.ExitTimeout`）を追加 | Issue #30 |
| 1.9 | 2026-11-02 | `agent` に組み込みのエージェント名の一覧（`Builtin`・`IsBuiltin`）を足し、`pane.Agents`・`setup.AgentNames`・`config.agentNames` をこれに 1 か所化（出力は変えない）。§4 の状態の決定に、`/proc` が読めないときの扱いを追記 | Issue #35・#36 |
| 2.0 | 2026-11-02 | §3 の公開 IF 列と §5.1 の型を実装の名前・シグネチャに合わせた（`agent.Get`・`updatecheck.Runner.Status`・`Registry.Lock`・`tmux.Runner` などの存在しない名前を除いた）。§2 の `templates/` から存在しない `dispatch.md` を除いた。§7 の `ci.yml` に linterly・release plan test・install script test を追記（出力は変えない） | Issue #41・#42・#44 |
| 2.1 | 2026-11-02 | §3 の `auth` の公開 IF を `Open(path, clk, logger)` に直した（Issue #54 で `logger` 引数が増えた。実装・設定は変えない） | Issue #58 |
| 2.2 | 2026-11-02 | §3 の `setup` の公開 IF を実装の `Options{Path, Out, Home, Prompter, IssueToken}` に合わせた（実装にない `Options` の項目の記述を除き、`HostCandidates()`・`Candidates(addrs)` を明記。実装は変えない） | Issue #59 |
| 2.3 | 2026-11-02 | §2 のツリーに `test/setup/`（実 tmux のペインで `setup` を起動して `send-keys` で確かめるテスト。NFR-TEST-13）を追加（実装は変えない） | Issue #66 |
| 2.4 | 2026-11-02 | §tmux の許可サブコマンドと `Paste` の流れに `delete-buffer -b <一意名>`（失敗時・中断時の後始末）を追加 | Issue #57 |
| 2.5 | 2026-11-02 | `auth.Client` に `AllowedSessions` を追加（`token add --sessions`）。新しいパッケージ `internal/session`（セッション ACL とセッション内のペイン解決）を追加。`tools` を 17 個から 18 個に（`sessions_list`）。§1 の依存図・§2 のツリー・§3 の表を更新 | セッション分割運用（許可セッションの分離） |
| 2.6 | 2026-11-03 | `tools` を 18 個から 19 個に（`file_get`。`ops_file.go`）。`safety` に `CheckFileGet`・`IsSecretName`、`mcpapi` の監査 `Event` に `Path`・`Size`・`HasSize`、`tools` に `Audit`・`WithAudit` を追加。§3 の表を更新 | Issue #82 |
| 2.7 | 2026-11-04 | `safety` に `IsBroadCWD`（ペインの `cwd` が `/`・ホームそのものか）を追加。`tools` の `file_get` の根の集め方で使う。§3 の表を更新 | Issue #82 |
| 2.8 | 2026-11-04 | `internal/tools` を `toolkit`（共有部品）・`view`（読み取り系 10 ツール）・`ops`（操作系 9 ツール）に分け、`tools` は `New`・`Registry` だけにした（Linterly のディレクトリ上限のため。ファイルの移動と公開名の大文字化だけで、振る舞いは変えない）。`Tool`・`Deps`・`Audit`・`ErrorText`・`Endpoint*` は `toolkit` の名前になった。§2 のツリー・§3 の表を更新（§1 の依存図の `tools` は 3 つのサブパッケージをまとめた箱） | Issue #82（PR #84） |
| 2.9 | 2026-11-04 | §3 の `tools` の `file_get` の記述に、先頭 12 バイトを先に読むことを追記。`file_get` を試すための差し込み口（`toolkit.Deps.FileGet`。本番は nil）を追加。§2 のツリーに `test/integration/filegetintegration/` を追加 | Issue #82（PR #84 の独立レビューの指摘） |
| 2.10 | 2026-11-04 | `cmd/swarm-console` に `token list` と `pair` を追加。新しいパッケージ `internal/pair`（`pair` の本体）、`app` に `TokenAdd`・`TokenRevoke`・`TokenList`（`cmd` から移した。linterly のディレクトリ行数のため）、`auth` に `ErrNameInUse`（`Add` の名前重複の判定）と `List`・`Info` を追加（P-92） | Issue #83 |
| 2.11 | 2026-11-08 | `tools` を 19 個から 20 個に（`dir_list`。`ops/dir.go`）。`safety` に `CheckDirList`・`IsListableDir`（`dir_list` の `path` の検証と、一覧に載せる候補の判定。`file_get`・`pane_start` と共通）。`pair` に `--scope report\|ops`（`ops` は `/ops` の `url`・`viewer-ops-…` の名前・警告）（P-98） | Issue #90 |
| 2.12 | 2026-11-09 | `pair` に `--qr`（`qr.go`。接続文字列の QR を標準エラーに出す。符号化は `skip2/go-qrcode`、テストの読み取りは `makiuchi-d/gozxing`）（P-99）。§4：Codex の `PickKeys` を数字キーのみから「Up/Down の差分、カーソルが読めないときだけ数字キー」に改めた（実画面で数字キーは押した時点で確定するため。P-22）。`agent.FindChoice` は折り返した選択肢の続き行をまたいで上の選択肢へ続ける | Issue #89・#4 |
| 2.13 | 2026-11-16 | 新しいパッケージ `internal/webhook`（`Notifier`・台帳・本文・送信）。`pane` に `Deps.Observer`（`SnapshotObserver`。`build` の直後に呼ぶ）、`config` に `Webhook`・`Secret`（伏せ字の型）・`WebhookStatePath`、`app` に `webhook.go`（`url` があるときだけ作って配線）を追加（FR-STATE-1〜7） | Issue #93 |
| 2.14 | 2026-11-16 | `webhook` を「ペインごとの回（`working`・`waiting`・`done`）」と「送る約束の outbox」に分けた。件は回と独立で、本文・送った回数を台帳（版 2）に持ち、再起動後も同じ本文・同じ `event_id` で続ける（at-least-once）。`pane.Snapshot` に `ServerStart`（tmux の起動時刻。変われば回を捨てる）、回の基準に最終報告のパス（変われば取り直す）、ペインは続けて 2 回見えなければ捨てる。`config.Webhook` の説明・確認順を直し、`Notifier` は `fmt`・`slog` に固定の文だけを出す | Issue #93（PR #94 のレビューの指摘） |
| 2.15 | 2026-11-16 | `config.Webhook` に `Sessions`・`Target(セッション名)`（表があればその送り先、無ければ `[webhook]`）を追加し、`buildWebhook` は設定の `sessions` を受けて表の名前を検証する。`webhook.Notifier` は `Config` 1 つのまま、件を作る時・送る時にペインのセッションで送り先（署名の鍵・トークン・再送の設定・クライアントの `Timeout`）を引く（FR-STATE-8）。台帳の形は変えない | Issue #95 |
| 2.16 | 2026-11-19 | `webhook` に asking の回（台帳の `asked`）と `pane.asking` の本文（`AskingPayload`）を追加（FR-STATE-9〜11）。`agent` に `Submitter`（`Submit` の行の Enter）・`Toggler`（複数選択の Space）の任意のインターフェースを足し、`omp` の `liveCard` は「最後の枠の上枠が Ask」に、`ParseCard` は v19 の形に直し、`pane/ops` の `PickAsk` が `Submit` の Enter・チェックの Space を送る | Issue #111 |
| 2.17 | 2026-11-20 | `agent` に `TurnProbe`（`Turn(pid, since)`。会話の記録の最後が番の途中かを読む。omp が実装）、`pane.Unaccepted` に `Export`・`Restore`・`Drop`・`Watch`、`webhook` の台帳に `watch` を追加。`confirmByLog` は番の途中なら時間で打ち切らず `working` と読む（FR-STATE-12。decisions.md P-108） | Issue #118 |
| 2.18 | 2026-11-20 | `agent` に `ReactionProbe`（`Reaction(pid, since, text)`。omp が実装）、`pane.PaneState` に `AgentPID`、`SendResult` に `Reaction`・`ReactionBasis`、`render.DispatchInput` に `FollowUp` を追加。`pane/ops` の `SendText` が作業中のペインで反応を待つ（FR-O2。decisions.md P-109） | Issue #120 |
| 2.19 | 2026-11-20 | `agent` に `Native`（`NativeTitle`・`NativeScreen`・`NativeDelivered`。omp が実装）と `NativeView`、`pane` に `ReadScreen`・`Reading`（`capture-pane` か、tern の画面なら会話の記録から組み立てた画面）、`pane.Tmux` と `tmux.Client` に `PaneMeta`（`display -p -t <id>` の pid・タイトル）を追加。`omp/transcript` パッケージ（会話の記録の読み取りと `ask` の引数の解析）。`ops` の `look` は `Reading` を返し、`ask_pick` は tern の画面の質問 1 つの番号選択だけキーを送る | Issue #121 |
| 2.20 | 2026-11-21 | `agent` に `StartOptions`（起動時の `model`・`thinking` の検査と起動行の組み立て）・`ValidModel`・`ValidThinking`。§5.2 `Start` に `opts`、`Dispatch` に `args.Start`（`idle` への指定は `start_options_not_applicable`）、`pane.StartResult`・`DispatchResult` に `StartCommand` | Issue #124 |
| 2.21 | 2026-11-22 | §2 のパッケージ構成に `agent/omp/transcript/`・`testdata/native/`・`testdata/config/` を足し、§4 の任意のインターフェースの列挙に `ReactionProbe` を足す（`internal/agent/agent.go` の Adapter 以外の公開インターフェースと合わせる） | Issue #131 |
| 2.22 | 2026-11-22 | §3 の `store` に `CheckDir(path, want, uid)`（`report_dir` の持ち主・リンクでないこと・親フォルダの検査。権限だけなら直す）を足し、`auth` に `Add`・`Revoke` が書く前の `<tokens.toml>.lock` の `flock`（`List` はロックしない）を足す | Issue #127・#128・#137・#145 |
