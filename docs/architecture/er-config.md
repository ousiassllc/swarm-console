# ER / データモデル：config.toml・profiles.toml・最終報告・自己更新のファイル

[ER / データモデル](er.md) の続き。§5 config.toml、§6 profiles.toml、§7 最終報告ファイル、§8 自己更新のファイルを定める。

改訂履歴は [ER / データモデル（本書）](er.md#改訂履歴) にまとめてある。

## 5. config.toml

```mermaid
erDiagram
    CONFIG ||--o{ SESSION : "sessions"
    CONFIG ||--o{ PANE_NAME : "pane_names"
    CONFIG ||--o{ AGENT_CONFIG : "agents"

    CONFIG {
        string listen_addr PK "tailnet IP:48120 かループバック（必須）"
        string tmux_socket "tmux -L のソケット名"
        array sessions "既定 [0]"
        string default_session
        string default_lang "ja | en"
        duration poll_interval "5s"
        duration snapshot_ttl "2s"
        duration stalled_after "15m"
        duration layout_warning_ttl "10m"
        duration accept_timeout "10s"
        duration start_timeout "30s"
        duration lock_timeout "60s"
        string title_prefix "agent: "
        array allowed_roots
        array denied_paths
        array file_roots
        array disk_mounts "host_status が見るマウント"
        string report_dir
        bool update_check "true"
        duration update_check_interval "24h（1h 以上）"
        bool usage_check "true"
        duration usage_interval "5m（1m 以上）"
        bool update_auto "true"
    }
    SESSION {
        string name
    }
    PANE_NAME {
        string target PK "session:window.pane_index"
        string name "^[A-Za-z][A-Za-z0-9_-]*$"
    }
    AGENT_CONFIG {
        string name PK "omp | claude | codex"
        string command "起動コマンド"
        string exit_command "終了コマンド"
        array process_match "プロセス名・引数の正規表現"
        string working_re "作業中の目印"
        string ask_re "質問カードの判定"
        string idle_re "入力待ちの目印"
        string spinner_chars
        string status_line_re "ステータス行の目印"
        string border_re "枠線の目印"
        string auth_file "認証ファイルのパス"
    }
```

| 検証 | 内容 |
|------|------|
| `listen_addr` | 必須。`0.0.0.0`・`::`・ホスト名ワイルドカードは拒否。tailnet の範囲（**ポートは 48120 だけ**。`100.64.0.1:8080`・`[fd7a:115c:a1e0::1]:1` などは拒否）またはループバック（どのポートでも可。ポート 0 も可）のみ（P-01・P-73）。ゾーン付き IPv6（`fe80::1%eth0` など）は拒否 |
| `tmux_socket` | 省略（既定のソケット）か `^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`（英数字で始まり 64 字まで） |
| `sessions` | 1 つ以上。重複なし。名前は空（空白だけを含む）・`.`・`:`・制御文字を含まない（tmux が `.`・`:` を `_` に置き換えて一致しなくなるため）。`default_session` は `sessions` のどれか。トークンの `allowed_sessions`（ER §3）は `sessions` の部分集合で、ツールが触れるセッションは両方の共通部分。`default_session` は位置の接頭辞を省略するセッションを決めるだけで、ツールの許可・`session` の既定には使わない（FR-SESS-12・P-87） |
| `default_lang` | 省略（`ja`）か `"ja"`・`"en"`。それ以外は起動拒否 |
| `pane_names` | キーは `<セッション>:<ウィンドウ>.<ペイン番号>`（`^(.+):([0-9]+)\.([0-9]+)$`）の形で、セッションは `sessions` のどれか（FR-POS-5）。名前は `^[A-Za-z][A-Za-z0-9_-]*$`（英字始まりなので tmux のペイン ID の `%` 始まりとは重ならない）で、自動名（`T1`・`B2`・`R12` など）と衝突せず、名前どうしも大文字小文字を区別せず重ならない。どれかに反したら起動拒否 |
| `allowed_roots` | 1 つ以上（空配列は起動拒否）。書かなければ既定を使う |
| `allowed_roots`・`file_roots`・`denied_paths`・`disk_mounts`・`report_dir` | 絶対パス（空文字・制御文字を含むもの・相対パスは不可）。`~` は `~` 単体（ホームそのもの）か `~/…` の形だけで、`~user` のような他ユーザー形式は不可。`~` は展開。`report_dir` は 1 つの文字列で、省略か空文字なら既定（`/tmp/swarm-console/reports`）。`allowed_roots`・`file_roots`・`denied_paths` の比較は `filepath.Clean` ＋ シンボリックリンク解決後。`denied_paths` は一致または配下を拒否（`cwd`・`file` の両方） |
| `denied_paths` の既定 | 既定は `["~/dotfiles"]`。`config.toml` に書いたら既定と**入れ替わる**（足されない。書き方の例は [環境構築](../environment/setup.md#ローカルで動かす)）。P-29 |
| 期間 | `time.ParseDuration` 形式。`poll_interval` ≥ 1s、`snapshot_ttl` ≥ 100ms、`update_check_interval` ≥ 1h、`usage_interval` ≥ 1m。`stalled_after`・`layout_warning_ttl`・`accept_timeout`・`start_timeout`・`lock_timeout` は 0 より大きい期間（下限は決めていない） |
| `title_prefix` | 改行・制御文字を含まない 1 行 |
| `update_check` | bool。省略は `true`。`false` なら `serve` は GitHub へ確認しに行かない（`version --check` の手動確認は動く。FR-UPD） |
| `update_auto` | bool。省略は `true`。`false` なら自己更新だけ止まる（確認と `host_status` は続く）。`update_check = false` のときは無視される（確認も更新もしない。FR-UPD-19） |
| `usage_check` | bool。省略は `true`。`false` なら `serve` は `omp usage --json` を実行せず、使用状況は `disabled`（FR-REP-12） |
| `usage_interval` | 期間。省略は `5m`、`1m` 以上。使用状況の取得の間隔。前の実行が終わるまで次を始めない |
| `agents.<name>` | `<name>` は `omp`・`claude`・`codex` だけ。書かれたキーだけ組み込みの既定を上書きする。正規表現（`working_re`・`ask_re`・`idle_re`・`status_line_re`・`border_re`・`process_match` の各要素）は起動時にコンパイルできること。`command`・`exit_command` は空文字不可で、改行・制御文字を含まない 1 行。`auth_file` は空文字不可（設定エラー）で、絶対パス（`~` 展開） |
| 未知のキー | 起動拒否（打ち間違いを検知する） |

### 5.1 `setup` による書き戻し

`swarm-console setup`（FR-SET）が `config.toml` に書く規則。目的は「手で書いた内容を壊さない」ことで、ファイル全体を作り直さない。設定の読み込み（`config.Load`）は変えない。

| 項目 | 規則 |
|------|------|
| 読み方 | `go-toml/v2` の `unstable.Parser` で、キー（表を含む完全なキー）ごとの値のバイト範囲と、各表のヘッダー・最後のキーの位置を取る（`Unmarshal` では位置が取れない）。値の解釈は通常の読み込み（`DisallowUnknownFields`）と別に行い、`setup` が聞かないキー（FR-SET-4）も含めて何も変えない |
| 何を書くか | 運用者が画面で値を変えたキーだけ。比較は検証・`~` 展開・Clean 後の実効値で行い、ファイルに無く既定のままのキー（例：触らなかった `denied_paths`）は書かない |
| 既にあるキーを変える | 値のバイト範囲だけを置き換える。キー・`=`・前後の空白・行末コメント・前後の行は 1 バイトも変えない |
| 複数行の配列 | 変えるなら配列全体（`[` から `]`）を 1 行の配列に置き換える。配列の中にコメントがあるときは「このキーの中のコメントは消える」と確認画面に出し、了承がなければそのキーは元のままにする |
| 無いキーを足す（最上位） | 最初の表ヘッダーの直前（その直前の空行は保つ）に `key = value` を 1 行足す。表が 1 つも無ければファイルの末尾 |
| 無いキーを足す（表の中。`[agents.omp]` など） | その表の最後のキーの次の行。表が無ければ、末尾に空行 1 つと表ヘッダー、キーを足す |
| 挿入先を決められない形 | インラインテーブルへの追加、同じ表が離れて 2 回出てくる、dotted key で別の表に分かれている、などは、何も書かずに中止し、該当キーを手で直すよう案内する（一部だけ書くことはしない） |
| 値の書き方 | 文字列は `"..."`（基本文字列）、配列は 1 行。足した行にコメントは付けない |
| 改行・末尾 | 既存ファイルの改行（LF・CRLF）と末尾の改行の有無に合わせる |
| 新規作成 | 全キーの説明と既定値をコメントで並べたひな形（`config.toml` の見本。`go:embed`）から始め、同じ規則で値を足す。ひな形は `setup` を通さなくても手で使える |
| 保存前の検査（どれか 1 つでも落ちたら何も書かない） | ① 結果を `serve` と同じ読み込み経路（`config.ParseBytes`）で読み直して通る ② 値を変えていないキーの実効値が読み込み時と同じ ③ 置き換えた範囲・足した行の外のバイト列が元と同一 |
| 競合 | 読み込み時の内容のハッシュを持ち、書く直前にファイルを読み直して違えば中止する（`serve` や手書きとの競合。上書きしない） |
| 書き込み | `store.WriteFileAtomic`（一時ファイル → fsync → rename）。既存ファイルの権限は変えない。新規は 600（「644 以下」を満たす） |
| シンボリックリンク | `config.toml` がシンボリックリンクなら書かずに中止する（rename でリンクが普通のファイルに変わる、リンク先が設定の置き場の外・`~/dotfiles` のことがあるため。NFR-SEC-14）。手で編集してもらう |
| 手書き専用のキー | 下記が手で書かれていても、`setup` は何も変えずに残す：`tmux_socket`・`default_lang`・期間のキー・`title_prefix`・`file_roots`・`disk_mounts`・`report_dir`・`[pane_names]`・`[agents.<name>]` の `command`・`exit_command`・`auth_file` 以外のキー |

## 6. profiles.toml

```mermaid
erDiagram
    PROFILES_FILE ||--|| COMMON : "common"
    PROFILES_FILE ||--o{ PROFILE : "profile.<name>"
    COMMON ||--o{ RULE : "rules"
    PROFILE ||--o{ RULE : "rules"

    PROFILE {
        string name PK "sweep | fix | investigate（追加可）"
        array rules "共通の条件に足す条件"
    }
    RULE {
        string text "1 行。{report_path} が使える"
    }
```

初期値（元の omp-dispatch の条件そのまま）：

```toml
[common]
rules = [
  "コミットは必ず `git commit -s` で行う（cherry-pick も `-s`）。マージは `git merge --signoff`。",
  "force push・rebase・amend・squash・reset・`--no-verify` はしない。フックが落ちたら止めて報告する。",
  "リモートブランチは作業用でも消さない（削除はローカルだけ）。",
  "マージ・Issue の close・デプロイはしない。",
  "トークン・パスワードなどの秘密をコード・ログ・PR・Issue に書かない。",
  "オーナーの checkout と `~/dotfiles` には触らず、worktree で作業する。",
  "CI が緑になったら止める。",
  "最終報告（PR・head・CI・Issue 番号・残りの判断）を {report_path} にも書く。",
]

[profile.sweep]
rules = ["PR は 1 本まで（`--single-pr`）。PR を出して CI が緑になったら止まる。"]

[profile.fix]
rules = ["PR は 1 本まで。バグの再現確認と回帰テストを含める。PR を出して CI が緑になったら止まる。"]

[profile.investigate]
rules = [
  "調査だけを行い、ファイルの変更・コミット・push・PR 作成はしない。",
  "分かったこと・根拠・残りの判断を報告する。",
]
```

| 検証 | 内容 |
|------|------|
| 必須 | `common` と、`sweep`・`fix`・`investigate` の 3 プロファイルが存在する（追加は可） |
| 規則 | 各 `rules` は空でない 1 行の文字列（改行不可）。プレースホルダーは `{report_path}` だけ。他の `{...}` は起動時エラー |
| 不在 | ファイルが無ければ、上記の初期値を組み込みで使う |

## 7. 最終報告ファイル

| 項目 | 内容 |
|------|------|
| パス | `<report_dir>/<位置>.md`（`<位置>` はセッション内の位置ではなく全体の位置。既定セッションは `T1`、それ以外は `1:T1`。位置の `:`・`/`・`\` はすべて `_`。`/`・`\` はパスの外へ出さないため。例 `work:B2` → `work_B2.md`、`1:T1` → `1_T1.md`、`a/b:T1` → `a_b_T1.md`）。別のセッションの `T1` と衝突しない（FR-SESS-7・P-82） |
| 書き手 | エージェント（指示文の最後の条件で書かせる）。サーバーは読むだけ（`report_read`） |
| 移動 | `dispatch`・`pane_close` が古いファイルを `<report_dir>/archive/<位置>-<unix 秒>.md` へ移す（サーバーが書く唯一の操作）。アーカイブ名の `<位置>` も上のパスと同じ規則（`:`・`/`・`\` → `_`。例 `a_b_T1-1730000000.md`） |
| 読み取り上限 | 20,000 字。超えたら先頭から切って `truncated: true` |
| 存在判定 | 通常ファイルがあれば `report_exists: true`。空ファイルは存在扱い。シンボリックリンクは辿らず「無し」（P-30） |

## 8. 自己更新のファイル

置き場は `~/bin/swarm-console/` に固定（FR-UPD-12・15。NFR-SEC-14・16）。サーバーが書くのは次の 5 つだけ。

| ファイル | 中身 | 書き手・消す人 |
|---------|------|--------------|
| `swarm-console` | 動いているバイナリ（権限 755） | 入れ替え（`rename`）と戻し（`rename`） |
| `swarm-console.new` | ダウンロード中・検証待ちの一時ファイル | 入れ替えが作り、`rename` するか失敗時に消す |
| `swarm-console.old` | 入れ替え前のバイナリ（ハードリンク。1 世代だけ） | 入れ替えのたびに作り直す。戻しで `swarm-console` に `rename` される |
| `swarm-console.update-pending` | 試運転中のマーカー | 入れ替えが作る。試運転が済む・戻す・`to` が自分の版でないときに消す |
| `swarm-console.update-failed` | 起動に失敗して戻した版の記録 | 戻しが書く。運用者が消すと同じ版を再び試す |

```json
// swarm-console.update-pending（権限 600）
{"from": "v0.1.3", "to": "v0.1.5", "boots": 0}
// swarm-console.update-failed（権限 600）
{"version": "v0.1.5", "at": "2026-11-01T12:30:00+09:00"}
```

| 項目 | 検証・意味 |
|------|-----------|
| `from`・`to`・`version` | `vX.Y.Z`（FR-UPD-3 の形）。読めない・形が違うファイルは無いものとして扱い、消す |
| `boots` | 0 以上の整数。`serve` が起動の最初に 1 増やして書き戻し、3 以上になる起動で戻す。試運転が済んだらマーカーごと消える |
| 書き込み | 一時ファイル → `rename`（NFR-AVL-3 と同じ作り）。権限 600 |
