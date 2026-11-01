# アーキテクチャ設計

要件は [機能要件](../requirements/functional.md)・[非機能要件](../requirements/non-functional.md)、データ構造は [ER / データモデル](er.md)、ツールの入出力は [API 仕様](../api/endpoints.md)、パッケージ構成は [コンポーネント設計](../components/overview.md) を参照する。

## 1. 全体構成

```mermaid
graph TD
    C1["MCP クライアント（box curl / Claude Code / Codex / Cursor ほか）"]

    subgraph Proc["swarm-console（Go 1 バイナリ・1 プロセス）"]
        direction TB
        HTTP["HTTP サーバー（tailnet IP:48120）"]
        AUTH["認証ミドルウェア（Bearer・窓口スコープ・監査ログ）"]
        MCPR["MCP サーバー /report"]
        MCPO["MCP サーバー /ops"]
        TOOLS["ツール層（17 ツール・prompts・resources）"]
        DOM["ドメイン層（ペイン台帳・状態判定・メモ・指示組み立て・レンダラ・安全ポリシー）"]
        AD["アダプタ層（omp / claude / codex）"]
        INF["インフラ層（tmux クライアント・/proc リーダー・ファイルストア・クロック）"]
        SAMP["サンプラー（バックグラウンド）"]
    end

    TM["tmux サーバー"]
    FS["設定・データ・最終報告ファイル"]
    PROC["/proc"]

    C1 --> HTTP --> AUTH
    AUTH -->|scope=report,ops| MCPR
    AUTH -->|scope=ops| MCPO
    MCPR --> TOOLS
    MCPO --> TOOLS
    TOOLS --> DOM
    DOM --> AD
    DOM --> INF
    SAMP --> DOM
    INF --> TM
    INF --> FS
    INF --> PROC
```

- 1 プロセスに 2 つの MCP サーバー（`/report`・`/ops`）を持つ。両者はツール定義の一部を共有し、`/report` には見るツールだけを登録する（`tools/list` に出さない）。
- ツール層は薄く、引数の検証・窓口に応じた権限・エラー整形だけを行う。ロジックはドメイン層に置く。
- アダプタ層が画面の読み方（状態・カード・飾り）を、インフラ層が tmux・`/proc`・ファイルの出入りを閉じ込める。ドメイン層はどちらにも直接触らない（インターフェース越し）。

## 2. 技術選定

| 項目 | 選定 | 理由 |
|------|------|------|
| 言語 | Go（1 バイナリ） | 元仕様。配布が単純で、systemd 常駐に向く |
| MCP | 公式 Go SDK（`github.com/modelcontextprotocol/go-sdk`） | 元仕様。Streamable HTTP・prompts・resources に対応 |
| HTTP | 標準 `net/http` | 窓口 2 つと認証だけで済む |
| tmux 操作 | `tmux` コマンド（`os/exec`、引数配列）。専用ソケットを `tmux_socket` で指定可 | 元仕様。`capture-pane`・`send-keys`・`load-buffer`・`paste-buffer`・`select-pane -T`・`display -p` |
| 設定・ひな形 | TOML（`github.com/pelletier/go-toml/v2`） | 人が編集する設定・トークン・プロファイル。コメントが書ける |
| メモ | JSON 1 ファイル（`encoding/json`） | 元仕様。500 件までで十分小さい |
| テンプレート | `text/template`（`go:embed` で既定を同梱） | レポート・指示文を言語ごとに差し替えるため |
| ログ | `log/slog`（JSON Lines） | 標準。journald で見る |
| Lint | `golangci-lint` | 元仕様の CI |
| トークン | 32 バイト乱数（`crypto/rand`）＋ SHA-256 保存 | 高エントロピーなので低速ハッシュは不要 |
| 配布 | GitHub Releases ＋ 手で入れ替え | 元仕様 |

DB は使わない（ER ドキュメントは永続ファイルのデータモデル）。外部サービスへ依存しない。

## 3. レイヤー構造

| レイヤー | 責務 | 主なパッケージ（コンポーネント設計 §2） |
|---------|------|------------------------------------|
| エントリ | CLI（`serve`・`token`・`version`）、設定読み込み、起動時検証 | `cmd/swarm-console` |
| トランスポート | HTTP、窓口ごとのルーティング、認証、リクエスト上限、監査ログ | `internal/server`、`internal/auth` |
| ツール | MCP のツール・prompts・resources の登録、引数検証、エラー整形 | `internal/tools`、`internal/mcpapi` |
| ドメイン | ペイン台帳・位置解決・スナップショット・状態判定・メモ・指示組み立て・レンダリング・安全ポリシー | `internal/pane`、`internal/notes`、`internal/render`、`internal/safety` |
| アダプタ | エージェントごとの画面解釈・キー操作 | `internal/agent`（`omp`・`claude`・`codex`） |
| インフラ | tmux 実行、`/proc`・ホスト情報、ファイル入出力、時計 | `internal/tmux`、`internal/host`、`internal/store`、`internal/clock` |

依存方向は上から下の一方向。ドメインはインフラ・アダプタを小さなインターフェース（`TmuxRunner`・`Adapter`・`Clock`・`FS`）越しに使い、テストでは実 tmux と擬似エージェントに差し替えずに使える。

## 4. スナップショット方式

`report` を「同じペインの状態ならどのクライアントからでも 1 文字も違わない」ようにし、`stalled` 判定を呼び出しの有無によらず動かすため、ペインの状態を **スナップショット** にまとめる（P-05）。

```mermaid
flowchart LR
    S["サンプラー（poll_interval 5s）"] --> T["tmux: list-panes / capture-pane（ペイン並列）"]
    T --> P["/proc: ペインのプロセス木"]
    P --> D["アダプタ: 状態・活動・カード・ハッシュ"]
    D --> H["ハッシュ履歴（changed_at）"]
    H --> SN["Snapshot（時刻・ホスト・ペイン配列・警告）"]
    SN --> R["report / panes_status / resources"]
    OP["操作ツール（送信後）"] -->|強制再取得| S
    CALL["読み取りツール"] -->|snapshot_ttl 2s より古ければ再取得| S
```

- スナップショットは不変。`Snapshot.At` が `report` の時刻になり、`elapsed`・`changed_ago` も `At` 基準で計算する。呼び出しごとの時刻・クライアント情報は出力に入れない。
- 読み取りツールは、スナップショットが `snapshot_ttl`（既定 2 秒）以内ならそれを返し、古ければ 1 回だけ（singleflight）取り直す。同時に来た複数クライアントは同じスナップショットを受け取る。
- 操作ツールは、送信後の確認のために対象ペインを `accept_timeout` までポーリングし、終わったらスナップショットを取り直す。
- 画面ハッシュは、アダプタが「揮発部分（スピナー・経過時間・トークン数の表示）」を取り除いた本文から計算する。ハッシュが変わった時刻を `changed_at` として覚え、`working` で `now - changed_at ≥ stalled_after` なら `stalled` にする。サーバー起動直後は `changed_at` を起動時刻とし、起動後 `stalled_after` 経つまで `stalled` にしない。
- 並びの差分（追加・削除・位置の変化）は前回のスナップショットと比べて検知し、`layout_changed` 警告を `layout_warning_ttl` の間出す。サーバー起動直後は比較対象がないので警告しない。
- ペインが 2 回連続のスナップショットで消えていたら、そのペインのメモを履歴へ移す（`closed_by: "vanished"`）。

## 5. 通信フロー

### 5.1 接続・認証

```mermaid
sequenceDiagram
    participant C as MCP クライアント
    participant S as swarm-console
    participant A as 認証ミドルウェア
    participant T as ツール層

    C->>S: POST /ops（Authorization: Bearer ...）
    S->>A: ヘッダー検証
    alt トークンなし・不一致
        A-->>C: 401 unauthorized
    else スコープ report で /ops
        A-->>C: 403 forbidden_scope
    else 有効
        A->>T: クライアント名・スコープをコンテキストに載せて渡す
        T-->>C: JSON-RPC 応答
    end
```

### 5.2 `dispatch`（空きペインへ指示）

```mermaid
sequenceDiagram
    participant C as クライアント
    participant T as ツール層
    participant D as ドメイン
    participant X as tmux
    participant N as ノートストア

    C->>T: dispatch(pane, agent, cwd, title, command, task, profile, ...)
    T->>D: 引数検証・窓口確認
    D->>D: ペイン解決・状態確認（shell / idle 以外は拒否）
    D->>D: 指示文を組み立て（command + task + 共通 + profile + extra_rules）
    D->>D: 文字・長さ・cwd・秘密文字列を検証（何も送る前に失敗させる）
    D->>D: ペインロック取得
    opt 状態が shell
        D->>X: select-pane -T / cd / エージェント起動
        D->>X: idle を待つ（start_timeout）
    end
    D->>D: 古い最終報告をアーカイブへ移動
    D->>X: load-buffer + paste-buffer -p → 1 秒待つ → Enter
    D->>X: 受理確認（working になる / 入力欄が空）
    D->>N: note を保存
    D-->>C: {ok, state, sent: 指示文全文, note_saved}
```

### 5.3 `ask_pick`（質問カードに答える）

```mermaid
sequenceDiagram
    participant C as クライアント
    participant D as ドメイン
    participant AD as アダプタ
    participant X as tmux

    C->>D: ask_pick(pane, n)
    D->>X: capture-pane
    D->>AD: ParseCard
    AD-->>D: カード（選択肢・カーソル）
    D->>AD: PickKeys(card, n)
    AD-->>D: キー列
    loop 最大 3 回
        D->>X: send-keys（キー列）
        D->>X: capture-pane
        D->>AD: ParseCard（カーソル確認）
    end
    alt カーソルが n
        D->>X: send-keys Enter
        D->>X: カードが閉じ working になるのを待つ
        D-->>C: {ok: true, state: working}
    else 合わない
        D-->>C: エラー cursor_mismatch（Enter は送っていない）
    end
```

## 6. 認証と窓口

```mermaid
flowchart TD
    R["リクエスト"] --> P{"パスは /report か /ops か"}
    P -- いいえ --> N404["404"]
    P -- はい --> H{"Authorization: Bearer あり"}
    H -- なし --> U["401 unauthorized"]
    H -- あり --> L["sha256 して全トークンと定数時間比較"]
    L --> M{"一致し、失効していない"}
    M -- いいえ --> U
    M -- はい --> SC{"窓口とスコープ"}
    SC -- "/ops かつ scope=report" --> F["403 forbidden_scope"]
    SC -- "/report かつ scope=report または ops" --> OK["通す"]
    SC -- "/ops かつ scope=ops" --> OK
```

- `/report` は `report`・`ops` どちらのトークンでも通る（`ops` は `report` を含む。P-03）。`/ops` は `ops` のみ。
- トークン照合は `tokens.toml` の更新時刻が変わっていたら再読み込みする（失効を再起動なしで効かせる。P-04）。
- 401・403 とも本文は `{"error":{"code":...,"message":...}}` の固定形。ヒントになる情報（トークンが存在するか等）は出さない。
- 通ったリクエストごとに、クライアント名・窓口・ツール・ペイン・結果コード・所要時間を JSON Lines で記録する。

## 7. デプロイ構成

| 項目 | 内容 |
|------|------|
| 実行場所 | ousiass-desktop（tmux サーバーと同じホスト、同じユーザー） |
| バイナリ | `~/bin/swarm-console/swarm-console`（GitHub Releases から手で入れ替え） |
| 設定 | `~/.config/swarm-console/`：`config.toml`・`profiles.toml`・`tokens.toml`（600）・`templates/`（任意） |
| データ | `~/.local/share/swarm-console/notes.json`（600） |
| 最終報告 | `/tmp/swarm-console/reports/<位置>.md`（エージェントが書く）、`archive/`（サーバーが移す） |
| 待ち受け | `config.toml` の `listen_addr`（ousiass-desktop の tailnet IP:48120） |
| 常駐 | systemd ユーザーサービス `swarm-console.service` |
| クライアント | box：`/workspace/sc.sh <tool> '<json>'`（SOCKS5 localhost:1055 経由の curl、既定 `/ops`）。パソコンの AI ツールは MCP 設定に URL とトークンを入れる |

`swarm-console.service` の骨子：

```ini
[Unit]
Description=swarm-console (MCP console for tmux agents)
After=network-online.target tailscaled.service

[Service]
ExecStart=%h/bin/swarm-console/swarm-console serve --config %h/.config/swarm-console/config.toml
Restart=always
RestartSec=2
TimeoutStopSec=15
UMask=0077

[Install]
WantedBy=default.target
```

- `listen_addr` は `tailscale ip -4` の値を設定に書く（コードでアドレスを探しにいかない。P-01）。tailscale がまだ上がっていなくても `bind` に失敗したら再試行して立ち上がるように、`Restart=always` に任せる。
- 更新手順：新バイナリを置く → `systemctl --user restart swarm-console` → `host_status` で確認。

## 8. 設定ファイルの全体像

`~/.config/swarm-console/config.toml`（キーの型と検証は [ER §5](er.md#5-configtoml)）。

| キー | 既定 | 意味 |
|------|------|------|
| `listen_addr` | （必須） | tailnet IP:48120 |
| `tmux_socket` | なし | tmux の `-L` ソケット名（テスト用） |
| `sessions` | `["0"]` | 対象セッション |
| `default_session` | `sessions` の先頭（1 つだけのとき） | 位置の接頭辞を省略するセッション |
| `default_lang` | `"ja"` | `report` の既定言語 |
| `poll_interval` | `5s` | サンプラー間隔 |
| `snapshot_ttl` | `2s` | 読み取りツールが再取得する閾値 |
| `stalled_after` | `15m` | 画面変化なしで `stalled` とする時間 |
| `layout_warning_ttl` | `10m` | 並び変更の警告を出し続ける時間 |
| `accept_timeout` / `start_timeout` / `lock_timeout` | `10s` / `30s` / `60s` | 操作の確認待ち・起動待ち・同ペイン待ち |
| `title_prefix` | `"kana: "` | `pane_start` のタイトル接頭辞 |
| `allowed_roots` | `["/mnt/nvme/", "/home/ousiass/dle/"]` | `cwd` を許可する根 |
| `denied_paths` | `["~/dotfiles"]`（書いたら入れ替え。P-29） | `cwd`・`file` を拒否するパス |
| `file_roots` | `["/tmp/", + allowed_roots]` | `send_text` の `file` を許可する根（P-13） |
| `disk_mounts` | `["/", "/mnt/nvme"]` | `host_status` が見るマウント |
| `report_dir` | `/tmp/swarm-console/reports` | 最終報告の置き場 |
| `[pane_names]` | なし | 位置の固定名 |
| `[agents.<name>]` | 組み込み | アダプタの上書き（コマンド・正規表現・認証ファイル） |

## 9. 提案した既定値と実装時確認事項

元仕様に書かれていない点を、実装に入るために埋めた既定値。悠太さんの OK のときに合わせて確認する。「確認」列が「実装時」のものは、実環境・実画面を見ないと決められないもの。

| ID | 論点 | 提案 | 確認 |
|----|------|------|------|
| P-01 | 待ち受けアドレス | `listen_addr` を設定に明記。`0.0.0.0`・`::`・未指定は起動拒否。tailnet の範囲（`100.64.0.0/10`、`fd7a:115c:a1e0::/48`）かループバックのみ許可（ループバックはテスト用） | OK 時 |
| P-02 | MCP のセッション | Streamable HTTP はステートレスで動かす（`Mcp-Session-Id` を持たない）。SDK が対応していなければ、セッション ID を発行しても状態を持たない構成にする | 実装時（SDK の対応） |
| P-03 | `ops` トークンで `/report` | 呼べる（`ops` は `report` を含む）。`report` トークンは `/ops` で 403 | OK 時 |
| P-04 | トークンの形式 | `sc_` ＋ base64url 32 バイト。`tokens.toml` に SHA-256 を保存。失効は削除でなく `revoked_at` を付ける（名前の再利用は不可）。ファイルの更新時刻が変わるたびに再読み込み。発行時に 1 度だけ平文を標準出力へ | OK 時 |
| P-05 | スナップショットとサンプラー | §4 のとおり。`poll_interval` 5 秒、`snapshot_ttl` 2 秒。画面ハッシュは揮発部分を除いた本文で計算 | OK 時 |
| P-06 | エージェントの特定 | `/proc` でペインのプロセス木をたどり、コマンド名・引数がアダプタの `process_match` に当たるかで判定。取れなければ画面の特徴で補う | 実装時（実プロセス名） |
| P-07 | ウィンドウが複数のとき | ウィンドウが複数のセッションは `w<index>.` を付ける。段は `pane_top`、列は同じ段の `pane_left` の順位。固定名のキーは `<セッション>:<ウィンドウ>.<ペイン番号>` | OK 時 |
| P-08 | 並び変更の警告 | 検知から `layout_warning_ttl`（10 分）の間、`panes_status`・`report` に出す。起動直後は出さない | OK 時 |
| P-09 | 操作と状態の許可表 | 機能要件 §4.2 の「許可する状態」のとおり（`send_text` は `idle`・`working`・`stalled`、`asking` は拒否） | OK 時 |
| P-10 | `ask_pick` の `text` | `n` と `text` は同時に指定できない。`text` を指定すると「その他」を選んで `text` を入れる | OK 時 |
| P-11 | 報告待ちと最終報告ファイル | 報告待ち ＝ `idle` かつ最終報告ファイルがある。`dispatch`・`pane_close` は古いファイルを `reports/archive/<位置>-<unix 秒>.md` へ移す。位置の `:` はファイル名で `_` にする | OK 時 |
| P-12 | エラー形式 | 事前条件・権限の失敗は MCP の `isError: true` ＋ 本文 `{"error":{"code","message","details"}}`。送信後に期待どおりでない場合は `ok: false` ＋ `state`。`message` は日本語の固定文。HTTP の 401・403・413 も同じ本文形式 | OK 時 |
| P-13 | `send_text` の制御文字と `file` | 許可する制御文字は `\n`・`\t` のみ（ESC・NUL・CR など拒否）。`file` の読み取りは `file_roots`（`/tmp/` と作業フォルダの根）の下のみ、シンボリックリンク解決後に判定、UTF-8 で 8000 字以内 | OK 時 |
| P-14 | 秘密文字列のパターン | `gh[pousr]_`＋20 字以上、`github_pat_`、`sk-`＋16 字以上、`eyJ`＋20 字以上、`AKIA`＋16 字、PEM 見出し（`-----BEGIN ... PRIVATE KEY-----`）、自分のトークン（`sc_`＋40 字以上） | OK 時 |
| P-15 | メモの置き去り | tmux サーバーの起動時刻を `notes.json` に持ち、変わっていたら現行メモを履歴へ（`closed_by: "tmux_restart"`）。ペインが 2 回連続で消えていたら履歴へ（`vanished`） | OK 時 |
| P-16 | `agent_auth_status` の読み先 | **確定**（[agent-auth.md](agent-auth.md)）。Claude Code：`~/.claude/.credentials.json` の `claudeAiOauth.expiresAt`・`refreshTokenExpiresAt`（epoch ミリ秒）。**`expiresAt` が 0 なら `refreshTokenExpiresAt` が未来でも `expired`**（`0` は無期限ではなく失効の値）。`expiresAt` が過去（> 0）で更新が未来なら通常の自動更新として `valid`。判定表は agent-auth.md。Codex：`~/.codex/auth.json` の `tokens.access_token`（JWT）の `exp`（秒）。omp：`[agents.omp] auth_file`（未設定なら「不明」）。読むのは期限だけ | **確定**（実機の Claude は `expiresAt: 0`＋トークン空で `expired`） |
| P-17 | 数量と文字数の上限 | 総ペイン 32、文字列項目 2000 字（`memo` 4000 字）、`items`・`rules` 50 件、`note_log` 1 行 500 字、リクエスト本文 256 KiB、`pane_tail` 20,000 字、`report_read` 20,000 字 | OK 時 |
| P-18 | 指示文の形 | `command` → 空行 → 「## やること」`task` → 空行 → 「## 条件」の箇条書き（共通 → プロファイル → `extra_rules`）。`{report_path}` は最終報告ファイルの実パスに置換 | OK 時 |
| P-19 | `pane_close` の許可状態 | `idle`・`shell` のみ。`working`・`stalled`・`asking`・`unknown` は拒否（元仕様は `working` のみ明記） | OK 時 |
| P-20 | リリース | タグ `v*` の push で linux/amd64 バイナリを GitHub Releases に載せるワークフロー | OK 時 |
| P-21 | テスト用の tmux | `tmux_socket`（`-L`）で専用サーバーを使う | OK 時 |
| P-22 | 状態判定・カードの正規表現の具体値 | [agent-patterns.md](agent-patterns.md) のとおり。既定値は組み込み、`[agents.<name>]` で上書き可。**omp は実画面で確定**。Claude Code は idle・信頼確認・`/model` が実画面、作業中・許可確認・質問は推定（ログイン切れで採れず）。**Codex は実画面で未確定**（この機に `codex` が無い。推定値） | **未完了**：Claude Code の作業中・許可確認・質問、Codex の全画面は、実画面で確かめて値を直す |
| P-23 | `dispatch` の `note` 保存 | 送信が行われた後に保存（拒否・事前検証失敗では保存しない）。受理に失敗しても保存し `note_saved: true` を返す | OK 時 |
| P-24 | tmux のバージョン | **確定**：3.x を前提。実機は `tmux 3.2a`（`load-buffer -`・`paste-buffer -p` の実動作を専用ソケットのテストで確認）。`tmux -V` を起動時に読み、`tmux.CheckVersion` が 2.x 以下・判定不能なら警告（3.x・`next-`・`master` は警告なし） | 実機で確定済み |
| P-25 | `report` の `format: json` と `pane` の併用 | `panes` と `attention` をその 1 ペインだけに絞る。`host`・`layout`・`warnings` は全体のまま | 実装時に決めた。OK 時に確認 |
| P-26 | `/report` で操作ツールを `tools/call` したとき | JSON-RPC `-32601` ではなく、`isError: true` ＋本文 `{"error":{"code":"unknown_tool",...}}` で返す（go-sdk v1.8.0 が `-32601` の `data` を捨てるため）。何も実行しない | 実装時に決めた。OK 時に確認 |
| P-27 | `layout`（段・列の数）が複数セッションのとき | 既定セッション（無ければセッション順で最初にペインのあるもの）の最初のウィンドウ | 実装時に決めた。OK 時に確認 |
| P-28 | `unknown`（エージェント無し）のペインへの操作 | `ask_pick`・`send_text`・`send_escape`・`pane_close`・`dispatch` は `invalid_state` ではなく `unsupported_agent`（API 仕様 §3.2 の表に合わせる） | 実装時に決めた |
| P-29 | `denied_paths` の既定 | **確定：既定 `~/dotfiles`、`config.toml` で変更可**。`denied_paths` を書いたら既定と入れ替わる（足されない）。書かなければ既定。`~` は展開して絶対パスで比較。`cwd`・`file` の両方に効く。悠太さんの checkout の一覧は仕様書に無いため、必要なら設定で書く（例は [環境構築](../environment/setup.md#ローカルで動かす)） | **確定** |
| P-30 | `report_read` の読み取り | 最終報告ファイルは `O_NOFOLLOW` で開く（シンボリックリンクなら `exists: false`）。認証ファイルなどを読み出されないため | 実装時に決めた |
| P-31 | 仕様書の行数管理 | **確定**：`.linterlyignore` で `docs/**` を Linterly の対象から外す（`api/endpoints.md`・`architecture/er.md` が 300 行を超えるため）。仕様書の分割は別 Issue（#3） | **確定** |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-31 | 新規作成 | 初版 |
| 1.1 | 2026-11-01 | P-16・P-24 を確定、P-22 を omp で確定（Claude Code は一部推定、Codex は未確定）。agent-patterns.md・agent-auth.md を追加。P-25〜P-31 を追加 | 実装時確認事項の反映（Issue #1） |
