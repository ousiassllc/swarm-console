# 機能要件

元仕様 v1.1 の §1〜§7・§10 を機能要件として整理したもの。引数・返り値の厳密な定義は [API 仕様](../api/endpoints.md)、データ構造は [ER / データモデル](../architecture/er.md) を参照する。

元仕様に書かれていない既定値は「提案」として `P-xx` の番号を付け、[アーキテクチャ設計 §9](../architecture/overview.md#9-提案した既定値と実装時確認事項) に一覧する。

## 1. ユースケース

```mermaid
graph TD
    OPSAGENT["運用エージェント（クライアント環境）"]
    PCAI["パソコンの AI ツール"]
    ROUT["ルーティン"]
    ADMIN["運用者"]

    OPSAGENT --> UC1["UC1 全ペインの状況を見る"]
    OPSAGENT --> UC2["UC2 質問カードに答える"]
    OPSAGENT --> UC3["UC3 指示を送る（dispatch）"]
    OPSAGENT --> UC4["UC4 ペインを閉じる・メモを履歴へ"]
    OPSAGENT --> UC5["UC5 ペインを引き継ぐ（メモ・履歴を読む）"]
    OPSAGENT --> UC6["UC6 作業を中断する（Esc）"]
    PCAI --> UC1
    PCAI --> UC2
    PCAI --> UC3
    ROUT --> UC1
    ADMIN --> UC7["UC7 トークン発行・無効化"]
    ADMIN --> UC8["UC8 サーバー状態・ログイン期限を確認"]
    ADMIN --> UC9["UC9 対話型で config.toml を作る・直す"]
    ADMIN --> UC10["UC10 新しい版があるか確認する"]
    OPSAGENT --> UC10
    OPSAGENT --> UC8
```

| ID | ユースケース | 主な機能 | 窓口 |
|----|------------|---------|------|
| UC1 | 全ペインの状況を、クライアントによらず同じ見た目で取る | `report`・`panes_status`・`pane_tail`・`report_read` | `/report` `/ops` |
| UC2 | `asking` のペインの質問カードを読み、番号（または自由入力）で答える | `ask_card`・`ask_pick` | カード読み取りは両方、選択は `/ops` |
| UC3 | 空き（`shell`）または入力待ち（`idle`）のペインに、決まった条件つきで指示を送る | `dispatch`・`pane_start`・`send_text` | `/ops` |
| UC4 | 完了したペインをきれいに閉じ、メモを履歴に移す | `pane_close`・`note_history` | `/ops` |
| UC5 | ペインを引き継ぐとき、依頼元・報告先・次にやることを会話を遡らず取る | `note_get`・`note_set`・`note_log`・`note_history` | 読みは両方、書きは `/ops` |
| UC6 | オーナーの OK があったとき、作業中のエージェントを止める | `send_escape` | `/ops` |
| UC7 | クライアントごとに別のトークンを発行・無効化する | `swarm-console token add/revoke`（サーバー上） | CLI |
| UC8 | ホストの負荷・ディスク・エージェントのログイン期限を見る | `host_status`・`agent_auth_status` | `/report` `/ops` |
| UC9 | 運用者が `config.toml` を、対話型の画面で作る・直す（手で書いた内容を残したまま。手書きのままでも使える） | `swarm-console setup` | CLI |
| UC10 | 動いているバイナリより新しい版が GitHub Releases にあるかを確認し、あれば `serve` が自分で入れ替える（自己更新。§12） | `swarm-console version --check`（確認だけ）・`serve` の定期確認と自動更新・`host_status` の `update` | CLI、`/report` `/ops` |

## 2. ペイン位置の決め方（FR-POS）

ペインの数・並び・セッションは決め打ちしない。呼び出し（またはスナップショット更新）のたびに tmux から今のペインを読み取る。

| ID | 要件 |
|----|------|
| FR-POS-1 | 対象のセッションは `config.toml` の `sessions` に並べる（既定は `["0"]`、複数可）。`default_session` は位置の接頭辞を省略するセッション（未指定で 1 つだけなら、それ） |
| FR-POS-2 | 位置の名前はペインの座標から自動で付ける。上の段から `T`・`B`（3 段以上なら `R1`・`R2`・`R3`…）、段の中で左から `1`・`2`・`3`…。段は `pane_top` の異なる値の順位、列は同じ段のペインの `pane_left` の順位で決める（P-07） |
| FR-POS-3 | 既定セッション以外は `<セッション>:T1` のように頭にセッション名を付ける |
| FR-POS-4 | 対象セッションにウィンドウが複数あるときは、位置の前にウィンドウ番号 `w<index>.` を付ける（例：`w1.T2`、`work:w1.B1`）。ウィンドウが 1 つだけのセッションは付けない（P-07） |
| FR-POS-5 | `config.toml` の `[pane_names]` でペインに固定の名前を付けられる（自動の名前より優先）。キーは `<セッション>:<ウィンドウ>.<ペイン番号>` |
| FR-POS-6 | ツールの引数 `pane` は位置（`"T1"`・`"work:B2"`）でも tmux ID（`"%0"`）でも受け付ける。位置は大文字小文字を区別しない。解決できなければ `pane_not_found` |
| FR-POS-7 | 並びが変わったとき、次の呼び出しから新しい位置で返し、`panes_status`・`report` に警告 `layout_changed` を出す。警告は検知から `layout_warning_ttl`（既定 10 分）残す（P-08） |
| FR-POS-8 | メモは tmux ID に結び付けて持つ。位置の名前が変わってもメモはずれない |

今の並び（横 4×縦 2）の例：

| 位置 | T1 | T2 | T3 | T4 | B1 | B2 | B3 | B4 |
|---|---|---|---|---|---|---|---|---|
| tmux ID | %0 | %1 | %2 | %3 | %4 | %5 | %10 | %12 |

## 3. ペインの状態とエージェントの種類（FR-STATE）

### 3.1 状態

| 状態 | 意味 | 主な判定材料 |
|------|------|-------------|
| `working` | エージェントが作業中 | アダプタの「作業中の目印」（スピナー、経過時間、`esc to interrupt`） |
| `asking` | 質問カードで止まっている | アダプタの質問カード判定 |
| `idle` | エージェントは起動していて、入力待ち | エージェントのプロセスがあり、上の 2 つに当たらない |
| `shell` | エージェントは起動しておらず、シェルのプロンプトだけ（空き） | ペインのプロセス木にエージェントがなく、前面コマンドがシェル |
| `stalled` | `working` に見えるが、画面が `stalled_after`（既定 15 分）変わっていない | `working` ＋ 画面ハッシュの無変化時間 |
| `unknown` | 判定できない | どれにも当たらない |

```mermaid
stateDiagram-v2
    [*] --> shell: ペインがシェルだけ
    shell --> idle: pane_start / dispatch でエージェント起動
    idle --> working: send_text / dispatch が受理された
    working --> asking: 質問カードが出た
    asking --> working: ask_pick でカードが閉じた
    working --> idle: 作業完了
    working --> stalled: 画面が 15 分変化なし
    stalled --> working: 画面が変化した
    working --> idle: send_escape で中断
    asking --> idle: send_escape でカードを閉じた
    idle --> shell: pane_close（終了コマンド）
    shell --> shell: pane_close（cd ~ / clear / タイトル消し）
    working --> unknown: 判定不能
    unknown --> idle: 判定可能になった
```

### 3.2 エージェントの種類（アダプタ）

エージェントごとの違いはアダプタに閉じ込め、ツール側はアダプタ越しにだけ画面を扱う。

| 項目 | omp | Claude Code | Codex |
|------|-----|-------------|-------|
| 起動 | `omp` | `claude` | `codex` |
| 終了 | `/exit` | `/exit` | `/quit` |
| 中断 | Esc | Esc | Esc |
| 質問カード | Ask カード（↑/↓ と Enter） | 選択肢つきの確認（↑/↓ または番号と Enter） | 承認の確認（番号と Enter） |
| 作業中の目印 | スピナーと経過時間 | スピナーと `esc to interrupt` | スピナーと `esc to interrupt` |

| ID | 要件 |
|----|------|
| FR-AD-1 | アダプタは「起動コマンド」「終了コマンド」「状態の判定」「質問カードの読み取り」「選択のキー操作」「画面の飾りの取り除き」を持つ |
| FR-AD-2 | 判定に使う文字列・正規表現は `config.toml` の `[agents.<名前>]` で上書きできる。エージェントの画面が変わったら設定だけで直せる |
| FR-AD-3 | どのアダプタにも当てはまらないペインは `agent: null`・`state: unknown` として扱い、`pane_tail`・メモ系だけ使える。それ以外の操作は `unsupported_agent` で拒否 |
| FR-AD-4 | 新しいエージェントはアダプタを 1 つ足せば対応できる（コード変更はアダプタ登録のみ） |
| FR-AD-5 | エージェントの特定は、まずペインのプロセス木（`/proc`）で行い、取れなければ画面の特徴で補う（P-06） |
| FR-AD-6 | `stalled` 判定の画面ハッシュは、スピナー・経過時間など毎回変わる部分を除いた本文で計算する（P-05） |

具体的な正規表現の値は [agent-patterns.md](../architecture/agent-patterns.md)（P-22。実画面サンプルは `testdata/screens/`）。

## 4. 機能一覧

返り値はすべて JSON（`report` の markdown 形式を除く）。長い文字列は上限で切り、切ったら `truncated: true` を付ける（上限値は API 仕様 §7）。

### 4.1 見る（`/report`・`/ops` の両方）

| ID | ツール | 内容 | 前提・拒否条件 |
|----|--------|------|--------------|
| FR-R1 | `panes_status` | 対象セッションの全ペインを 1 回で返す（位置・ID・エージェント・状態・タイトル・cwd・経過・活動・画面変化からの時間・メモ要約）。ホスト負荷・並びの警告・`layout`（段と列の数）も返す | `session` 指定が対象外なら `invalid_args` |
| FR-R2 | `pane_tail` | 1 ペインの最後の n 行を、枠線・TODO 表示・ステータス行を除いた本文だけで返す。`raw: true` なら飾りも残す。`lines` 既定 30・上限 200 | `unknown` エージェントでも可（飾りの除去はしない＝`raw` 相当） |
| FR-R3 | `ask_card` | 質問カードの質問・選択肢・カーソル・自由入力の可否を返す。`asking` でなければ `{"asking": false}` | `unsupported_agent`（`unknown`）|
| FR-R4 | `report_read` | `/tmp/swarm-console/reports/<位置>.md`（`:` は `_`）を読む。なければ `{"exists": false}` | — |
| FR-R5 | `host_status` | 負荷・CPU 数・メモリ・ディスク・tmux セッションの有無・エージェント種別ごとのペイン数・稼働時間・新しい版の確認結果（`update`。FR-UPD-9） | — |
| FR-R6 | `agent_auth_status` | エージェントごとのログイン状態（有効／期限切れ／48 時間以内に切れる／不明）と期限日時。鍵・トークンの中身は出さない | — |
| FR-R7 | `note_get` | 1 ペインのメモを全部返す | — |
| FR-R8 | `note_history` | 閉じたペインのメモを新しい順に返す。`repo`・`from`・`items` で絞り込み | — |
| FR-R9 | `report` | 全ペインの状況をサーバーが作った markdown で返す（§5） | — |

### 4.2 操作する（`/ops` のみ）

どの操作も、送ったあとに画面を確かめて、思った状態になったかを `ok` と `state` で返す。

| ID | ツール | 内容 | 許可する状態 | 拒否 |
|----|--------|------|------------|------|
| FR-O1 | `ask_pick` | `n` 番を選ぶ。カーソルを n に動かし、合っているか確かめてから Enter。`text` があれば「その他」を選んで文字を入れて確定（§6.1）。選んだあと、カードが閉じて `working` になったかを返す | `asking` | `not_asking`・`option_out_of_range`・`custom_not_allowed`・`cursor_mismatch`（Enter を送らずに中止） |
| FR-O2 | `send_text` | `text`（上限 8000 字）または `file`（サーバー上のパス）を、bracketed paste で貼り付け、1 秒待って Enter。入力欄が空になって `working` になったかを返す。制御キーは送れない | `idle`・`working`・`stalled`（エージェントが動いているとき）| `invalid_state`（`shell`・`asking`・`unknown`）・`forbidden_control_char`・`text_too_long`・`forbidden_path`・`file_unreadable` |
| FR-O3 | `send_escape` | `confirm: true` が必須。作業中のエージェントを中断する。使ったことをメモの履歴（`log`）に自動で残す。**オーナーの OK があるときだけ使う**（ツールの説明にも書く）。クライアントごとの許可設定は持たない | `working`・`stalled`・`asking`・`idle` | `confirm_required`・`invalid_state`（`shell`・`unknown`）|
| FR-O4 | `pane_start` | `shell` のペインで `cwd` に cd し、エージェントを起動、タイトルを `agent: <title>` にして、`idle` になったかを返す | `shell` | `invalid_state`・`forbidden_path`・`invalid_args`（`agent` が未設定のもの）|
| FR-O5 | `pane_close` | 終了コマンド → `cd ~` → `clear` → タイトル消し、を 1 つずつ「文字列 → 1 秒 → Enter → 1 秒」で送る。最後に画面が `~` のプロンプトだけになったかを確かめて返す。確かめられたら、そのペインのメモを履歴に移す | `idle`・`shell` | `invalid_state`（`working`・`stalled`・`asking`・`unknown`。P-19）|
| FR-O6 | `dispatch` | 指示を中身と条件に分けて受け取り、決まった形の指示文に組み立てて送る（§7）。送った指示文の全文を返す | `shell`（→ `pane_start` してから）・`idle` | `invalid_state`（`working`・`asking` ほか）|
| FR-O7 | `note_set` | 渡した項目だけ書き換える。`null` でその項目を消す | — | `secret_detected`・`invalid_args` |
| FR-O8 | `note_log` | 出来事を時刻つきで 1 行足す。直近 50 行を残す | — | `secret_detected`・`invalid_args` |

共通要件：

| ID | 要件 |
|----|------|
| FR-O-C1 | 同じペインへの操作は前の操作が終わるまで待たせる（同時に 2 つ送らない）。待ち時間が `lock_timeout`（既定 60 秒）を超えたら `busy_timeout` |
| FR-O-C2 | 事前条件（状態・パス・引数）を満たさないものは何も送らずに拒否する。送信後に期待した状態にならなかった場合は例外にせず `ok: false` と現在の `state` を返す |
| FR-O-C3 | 画面の確認は上限付きのポーリング（`accept_timeout` 既定 10 秒、`start_timeout` 既定 30 秒）で行い、固定時間の待ちに頼らない（ただし仕様で定めた「1 秒待って Enter」を除く） |
| FR-O-C4 | 送れる文字は通常の文字と Enter だけ。Esc は `send_escape` だけ。その他の制御キーは拒否する（`text` の ESC・NUL・CR などを含む制御文字は `forbidden_control_char`。`\n`・`\t` は許可） |

## 5. レポート（FR-REPORT）

### 5.1 要件

| ID | 要件 |
|----|------|
| FR-REP-1 | `report` は全ペインの状況をサーバーが組み立てた markdown で返す。クライアントは手を加えずそのまま表示する（ツールの説明に書く） |
| FR-REP-2 | 中身は、見出し（時刻・サーバー負荷）、ペインの表（位置・エージェント・状態・依頼元・リポジトリ／Issue・いまやっていること・次にやること）、要対応の一覧（質問カードで止まっている・止まっているように見える・完了して報告待ち） |
| FR-REP-3 | 状態は絵文字つきの決まった言葉で出す（下表） |
| FR-REP-4 | 引数：`format`（`markdown` 既定・`json`）、`pane`（1 ペインの詳しい版）、`lang`（`ja` 既定・`en`）。既定の言語は `default_lang` で変えられる |
| FR-REP-5 | テンプレートは言語ごとに `templates/report.ja.md`・`templates/report.en.md`。サーバーの設定フォルダ（`~/.config/swarm-console/templates/`）に置けば組み込みのものより優先され、直せばどのクライアントにも同じく反映される |
| FR-REP-6 | メモの中身（依頼元・次にやること など）は書かれたままの言葉で出す（訳さない） |
| FR-REP-7 | 同じスナップショットからは、クライアントによらず 1 文字も違わない出力を返す（時刻はスナップショットの時刻を使い、呼び出し時刻・クライアント名などを含めない）。受け入れ確認 §8 の条件 |
| FR-REP-8 | 「完了して報告待ち」は、状態が `idle` で、最終報告ファイルが存在するペイン（P-11） |
| FR-REP-9 | `format: json` は `panes_status` と同じペイン配列に要対応の一覧（`attention`）を足した JSON を返す。言語に依存しない |

### 5.2 状態の言葉

| 状態 | 日本語（`ja`） | 英語（`en`） |
|------|---------------|-------------|
| `working` | 🟢 作業中 | 🟢 Working |
| `asking` | 🟡 質問待ち | 🟡 Asking |
| `idle` | ⚪ 入力待ち | ⚪ Idle |
| `shell` | ⬜ 空き | ⬜ Free |
| `stalled` | 🔴 止まってそう | 🔴 Stalled |
| `unknown` | ❔ 不明 | ❔ Unknown |

### 5.3 出力レイアウト（`ja`、例）

````markdown
# swarm-console レポート

- 時刻: 2026-10-31 22:10:05 (+09:00)
- サーバー負荷: load1 98.2 / メモリ空き 41.3 GB

## ペイン

| 位置 | エージェント | 状態 | 依頼元 | リポジトリ / Issue | いまやっていること | 次にやること |
|------|------------|------|--------|------------------|------------------|------------|
| T1 | omp | ⚪ 入力待ち | — | — | — | — |
| B4 | omp | 🟢 作業中 | agent-b | ousiassllc/funid #982 | Running go test ./... | 完了したら #983 を入れる |
| B1 | — | ⬜ 空き | — | — | — | — |

## 要対応

- 🟡 質問待ち: T2 — (2) の Go での範囲をどうしますか？
- 🔴 止まってそう: B3 — 変化なし 20m
- 📩 報告待ち: T1 — 最終報告あり（`report_read` で読む）

## 警告

- 並びが変わりました: B3 が追加されました
````

- 要対応がなければ `- なし`。警告がなければ「警告」見出しごと出さない。
- 表のセルは `|` と改行をエスケープし、空は `—`。長いセルは 80 字で `…` に切る。
- `en` は見出し・ラベル・状態の言葉が英語になり、メモの中身は同じ。
- `pane` 指定の詳しい版は、上記に加えて「メモ全項目」「質問カード（あれば）」「画面の最後の 15 行（飾りなし）」「最終報告ファイルの有無と更新時刻」を出す。

## 6. 質問カードの操作（FR-ASK）

### 6.1 `ask_pick` の手順

```mermaid
flowchart TD
    A["ask_pick(pane, n | text)"] --> B{"状態は asking か"}
    B -- いいえ --> E1["not_asking"]
    B -- はい --> C["ask_card を読む"]
    C --> D{"n が範囲内か / text なら allows_custom か"}
    D -- いいえ --> E2["option_out_of_range / custom_not_allowed"]
    D -- はい --> F["アダプタが必要なキー列を作る（↑↓ または番号）"]
    F --> G["キーを送る"]
    G --> H["カードを再読み取りしてカーソル位置を確認"]
    H --> I{"目的の位置か"}
    I -- いいえ、再試行回数内 --> F
    I -- いいえ、上限超え --> E3["cursor_mismatch（Enter は送らない）"]
    I -- はい --> J{"text があるか"}
    J -- はい --> K["「その他」欄に text を貼り付け"]
    J -- いいえ --> L["Enter"]
    K --> L
    L --> M["accept_timeout までカードが閉じ working になるのを待つ"]
    M --> N["ok と state を返す"]
```

- `n`（選ぶ番号）と `text`（「その他」に打つ文字）は同時に指定できない。どちらも指定しなければ `invalid_args`。`text` を指定すると「その他」の選択肢を選んで `text` を入れる（P-10）。
- 再試行は 2 回まで。カーソルが合わないままの Enter は送らない。

## 7. 指示の組み立て（FR-DISPATCH）

| ID | 要件 |
|----|------|
| FR-DIS-1 | 引数：`pane`、`agent`、`cwd`、`title`、`command`（1 行目のスラッシュコマンド）、`task`（本文）、`profile`（`sweep`・`fix`・`investigate`）、`extra_rules`（追加の条件）、`note`（メモ。一緒に保存する） |
| FR-DIS-2 | 指示文は `command` ＋ `task` ＋ 条件（共通の条件 → プロファイル固有の条件 → `extra_rules`）の決まった形に組み立てる（API 仕様 §5.6）。`task` と `extra_rules` 以外にサーバーが足すのは、固定の見出しとプロファイルの条件文だけ。人の名前・経緯は足さない |
| FR-DIS-3 | 条件のひな形は `profiles.toml` に置く。初期値は元の omp-dispatch の条件そのまま（下表） |
| FR-DIS-4 | ペインが `shell` なら `pane_start` してから送り、`idle` ならそのまま送る。`working`・`asking` ほかは拒否（何も送らない） |
| FR-DIS-5 | 返り値は送った指示文の全文と、受け付けたか。同じ引数なら、どのクライアントから送っても、ペインに入る指示文は同じ |
| FR-DIS-6 | `note` は送信後に `note_set` と同じ検証・マージ規則で保存する（P-23）。保存したかを返り値の `note_saved` に入れる |
| FR-DIS-7 | 送信前に、そのペインの古い最終報告ファイルをアーカイブへ移す（P-11） |
| FR-DIS-8 | 組み立てた指示文は 8000 字以内（超えたら `text_too_long`、何も送らない） |

### 7.1 条件の初期値

共通の条件（全プロファイル）：

| # | 条件 |
|---|------|
| 1 | コミットは必ず `git commit -s`（cherry-pick も `-s`）、マージは `git merge --signoff` |
| 2 | force push・rebase・amend・squash・reset・`--no-verify` はしない。フックが落ちたら止めて報告する |
| 3 | リモートブランチは作業用でも消さない（削除はローカルだけ） |
| 4 | マージ・Issue の close・デプロイはしない |
| 5 | トークン・パスワードなどの秘密をコード・ログ・PR・Issue に書かない |
| 6 | オーナーの checkout と `~/dotfiles` には触らず、worktree で作業する |
| 7 | CI が緑になったら止める |
| 8 | 最終報告（PR・head・CI・Issue 番号・残りの判断）を `report_read` のファイル（`/tmp/swarm-console/reports/<位置>.md`）にも書く |

プロファイル固有の条件（共通に足す）：

| プロファイル | 足す条件 |
|-------------|---------|
| `sweep` | `--single-pr` で PR は 1 本まで。PR を出して CI が緑になったら止まる |
| `fix` | PR は 1 本まで。バグの再現確認と回帰テストを含める。PR を出して CI が緑になったら止まる |
| `investigate` | 調査だけを行い、ファイルの変更・コミット・push・PR 作成はしない。分かったこと・根拠・残りの判断を報告する |

## 8. 受け入れ確認（元仕様 §10）

| ID | 確認内容 | 検証方法 |
|----|---------|---------|
| AC-1 | 同じペインの状態で、Claude Code・Codex・Cursor・curl（クライアント環境）の 4 つから `report` を呼び、返る markdown が 1 文字も違わない（`lang` が `ja` でも `en` でも） | 4 クライアントの実接続 ＋ 自動テスト（同一スナップショットを固定したレンダリングの一致） |
| AC-2 | `/report` の `tools/list` に操作のツールが出ない。`report` のトークンで `/ops` を呼ぶと 403 | 自動テスト ＋ 実接続 |
| AC-3 | ペインを増やす・減らす・別セッションを足したとき、次の `panes_status` で新しい並びと警告が出て、メモがずれない | 実 tmux の統合テスト ＋ 実機 |
| AC-4 | 同じ `dispatch` の引数を 2 つのクライアントから送り、ペインに入る指示文が同じ | 実 tmux ＋ 擬似エージェントで貼り付け内容を比較 |
| AC-5 | 認証なし・違うトークン・窓口違い・許可していないフォルダ・作業中のペインを閉じる・Esc の `confirm` なし、がどのクライアントからでも同じエラーになる | 自動テスト ＋ 実接続 |
| AC-6 | 非機能要件 §7 のテスト（実 tmux、擬似画面、拒否、メモの書き込み途中停止）が全部通り、CI が緑 | CI |

## 9. メモ（FR-NOTE）

メモの目的：運用エージェントがペインを引き継ぐときに、毎回会話を遡らなくて済むようにする。項目定義は [ER / データモデル](../architecture/er.md#2-notesjson)。

| ID | 要件 |
|----|------|
| FR-NOTE-1 | 項目はすべて任意。分からないものは空のままでよい |
| FR-NOTE-2 | 項目：依頼元 `from`、報告先 `report_to`、リポジトリ `repo`、Issue・PR `items`、作業フォルダ `worktree`、ブランチ `branch`、指示ファイル `instruction`、OK の原文 `approval`、守ること `rules`、いまの状況 `status`、次にやること `next`、再開 ID `resume_id`、自由メモ `memo` |
| FR-NOTE-3 | `panes_status` と `report` には `from`・`repo`・`items`・`next` だけを要約として出す |
| FR-NOTE-4 | `note_set` は渡した項目だけ書き換え、他の項目は残す。`null` を渡すとその項目を消す |
| FR-NOTE-5 | `note_log` は出来事を時刻つきで 1 行足す（例：「Ask で B を選んだ」「agent-b へ報告した」）。直近 50 行を残す |
| FR-NOTE-6 | `pane_close` はそのペインのメモを履歴に移し、現行メモを消す |
| FR-NOTE-7 | 履歴は 500 件まで。古いものから消す |
| FR-NOTE-8 | 秘密の値は書かない。`ghp_`・`sk-`・`eyJ` で始まる長い文字列、PEM などに見える文字列が入っていたら書き込みを拒否（`secret_detected`。拒否した値は応答・ログに出さない） |
| FR-NOTE-9 | 書き込みは一時ファイルに書いてから rename（途中で落ちても壊れない）。権限 600 |
| FR-NOTE-10 | `send_escape` の実行は `note_log` に自動で残す |
| FR-NOTE-11 | tmux ペインが外部で消えた・tmux サーバーが再起動して ID が再利用され得る場合、旧メモを履歴へ移す（P-15） |

## 10. 安全のための決まり（FR-SAFE）

| ID | 決まり | 内容 |
|----|--------|------|
| FR-SAFE-1 | シェル実行なし | 決まった tmux 操作と、決まったファイル・`/proc` の読み取りだけ |
| FR-SAFE-2 | ペインを壊さない | ペインの kill、セッションの削除、ペインの分割はできない |
| FR-SAFE-3 | 起動できるもの | 設定したエージェント（omp・claude・codex）だけ |
| FR-SAFE-4 | 作業フォルダ | `/mnt/nvme/` の下と `~/projects/` の下だけ許可。オーナーの checkout（設定に一覧）と `~/dotfiles` は拒否 |
| FR-SAFE-5 | 送れる文字 | 通常の文字と Enter だけ。Esc は `/ops` の `send_escape`（`confirm` 必須）だけ。その他の制御キーは拒否 |
| FR-SAFE-6 | 窓口 | `/report` からは何も変えられない（メモの書き込みも不可）。`/report` の `tools/list` に操作のツールは載らない |
| FR-SAFE-7 | 作業中は閉じない | `pane_close` は `working` のペインを拒否 |
| FR-SAFE-8 | ログ | 誰が・いつ・どのツールを・どのペインに使ったかを記録する。送った本文とトークンは記録しない |
| FR-SAFE-9 | 呼び出しの間隔 | 同じペインへの操作は前の操作が終わるまで待たせる |

## 11. 対話型の設定（FR-SET）

`config.toml` を手で書く今の方法はそのまま残し、対話型（`swarm-console setup`）でも同じファイルを作る・直せるようにする。置き場は 1 つ（`~/.config/swarm-console/config.toml`）で、どちらで書いても `serve` が読む内容は同じ。書き戻しの規則は [ER §5.1](../architecture/er.md#51-setup-による書き戻し)、画面を作る部品は [コンポーネント設計](../components/overview.md#3-コンポーネントの責務) を参照する。

| ID | 要件 |
|----|------|
| FR-SET-1 | `swarm-console setup [--config <path>]`。ファイルが無ければ新規作成、あれば読み込んで現在値を初期値にして開く（初回専用ではなく、編集にも使う）。`--config` の既定は `serve`・`token` と同じ |
| FR-SET-2 | 設定の置き場は `serve` が読む `config.toml` だけ。`setup` 専用の別ファイルは持たない。手で書いた内容は次に `setup` で開いたときも読み込まれる |
| FR-SET-3 | 手で書いた値を壊さない。`setup` が触るのは、運用者が画面で値を変えたキーだけ。それ以外のキー・コメント・空行・並び・書式はバイト単位で同じに残す（規則は ER §5.1）。値が変わっていないキー、ファイルに無く既定のままのキーは書かない（既定値を書き出さない） |
| FR-SET-4 | 聞く項目と順番は下表。下表に無いキーは画面に出さず、読み書きとも一切触らない（手で書く） |
| FR-SET-5 | 入力の検証は `serve` の起動時と同じ関数を使う（`0.0.0.0`・`::`・ホスト名・ポート無し・tailnet 外は `listen_addr` として拒否など。ER §5 の検証表と同一）。入力中にその場でエラーを出し、通るまで次へ進めない。保存の直前に、結果のファイルを `serve` と同じ読み込み経路で読み直し、落ちるなら何も書かない |
| FR-SET-6 | 端末（TTY）でないとき（標準入力か標準出力が端末でない、`TERM=dumb`。SSH の `-t` なし・CI・パイプ）は、何も読まず何も書かず、理由と手書きの案内を標準エラーに出して終了コード 1 で終わる |
| FR-SET-7 | 中止（Ctrl+C・Esc）では何も書かない。終了コード 1 |
| FR-SET-8 | 既存の `config.toml` が TOML として読めない・未知のキーがある場合は、開かずに行番号・キー名つきのエラーで終わる（自動では直さない）。値の検証エラー（`listen_addr` 未設定を含む）は「現在値が不正」と示したうえで開き、その項目を直させる |
| FR-SET-9 | 保存したら、保存先と変更したキーの一覧を表示し、`systemctl --user restart swarm-console` で反映することを案内する（再起動は実行しない。NFR-OPS-3） |
| FR-SET-10 | 最後に任意で、トークンを発行できる（名前と `ops`/`report`）。`config.toml` の保存が済んだあとに `token add` と同じ処理で発行し、平文は標準出力へ 1 度だけ出す。発行の失敗は保存済みの `config.toml` を変えない |

### 11.1 聞く項目と順番

| # | 項目 | 聞き方 | 補足 |
|---|------|--------|------|
| 1 | `listen_addr` | このホストのネットワークインターフェースのうち tailnet の範囲（`100.64.0.0/10`・`fd7a:115c:a1e0::/48`）にあるアドレス ＋ ポート `48120` を選択肢に出す。末尾に「手で入力」（ループバックなど） | 現在値があれば最初に選ばれる。候補が 0 件なら手入力だけにし、tailscale が上がっていないかもしれないと案内する。`tailscale` コマンドは実行しない |
| 2 | `sessions`・`default_session` | `sessions` はカンマ区切りの 1 行。2 つ以上のときだけ `default_session` を選ぶ | 既定 `["0"]` |
| 3 | `allowed_roots` | 1 行 1 パスの複数行入力 | 許可する `cwd` の根。既定 `/mnt/nvme/`・`~/projects/` |
| 4 | `denied_paths` | 1 行 1 パスの複数行入力。初期値は今の実効値（ファイルに無ければ既定の `~/dotfiles`） | 画面に「書くと既定と入れ替わる（足されない）」と出す（P-29）。`~/dotfiles` を守るなら残す |
| 5 | エージェント（`omp`・`claude`・`codex`） | まず「エージェントの設定を変えますか」（既定は いいえ）。はい のときだけ、各エージェントの `command`・`exit_command`・`auth_file` を聞く | 空欄は「上書きしない」（組み込みの既定を使う）。既定値は入力欄の例示として出す |
| 6 | トークン発行（任意） | 発行するか → 名前 → `ops`/`report` | FR-SET-10 |

`setup` が聞かず手書きだけに残すもの：`tmux_socket`・`default_lang`・期間のキー（`poll_interval` ほか 6 つ）・`title_prefix`・`file_roots`・`disk_mounts`・`report_dir`・`[pane_names]`・`[agents.<name>]` の正規表現とその他のキー（`process_match`・`working_re`・`ask_re`・`idle_re`・`spinner_chars`・`status_line_re`・`border_re`）・`profiles.toml`・レポートのテンプレート。これらが手で書かれていても、`setup` は読み込んだうえでそのまま残す。

### 11.2 `setup` の流れ

```mermaid
flowchart TD
    S["setup 起動"] --> T{"TTY か"}
    T -->|いいえ| E1["理由を出して終了 1（何も読み書きしない）"]
    T -->|はい| R{"config.toml がある"}
    R -->|無い| N["ひな形から始める"]
    R -->|ある| L["読み込み（構文・未知キーで終了、値の不正は直させる）"]
    N --> F["項目 1〜5 を順に聞く"]
    L --> F
    F --> D["変更したキーだけを編集操作にする"]
    D --> V{"読み直して検証が通るか"}
    V -->|いいえ| E2["何も書かずエラーで終了 1"]
    V -->|はい| C{"読み込み後にファイルが変わっていないか"}
    C -->|変わった| E3["上書きせず中止 1"]
    C -->|同じ| W["一時ファイル → fsync → rename で保存"]
    W --> K["項目 6：トークン発行（任意）"]
```

## 12. 新しい版の確認と自己更新（FR-UPD）

GitHub Releases（`ousiassllc/swarm-console`）の最新版を見て、動いているバイナリより新しい版があるかを確認し、あれば `serve` が自分でダウンロード・検証・入れ替えをする（自己更新）。参考：同じ組織の mam（`ousiassllc/mam`）の `backend/internal/service/updater`（`/releases/latest` の取得・semver 比較・失敗の分類）。

**更新の方式は自己更新で確定（Issue #7 のヒアリング。P-46）。** 確認は §12.1〜12.3、自己更新は §12.4〜12.6。リリースを作る側（タグの付け方・アセット名・`SHA256SUMS` の書式）は [環境構築 CI・リリース](../environment/setup.md#cicd) と Issue #8 が決め、本節の FR-UPD-13・14 と同じにする。

### 12.1 確認の要件

| ID | 要件 |
|----|------|
| FR-UPD-1 | 確認先は `GET https://api.github.com/repos/ousiassllc/swarm-console/releases/latest` だけ。owner・repo は定数で、設定で変えられない。認証なし（トークンを送らない。`SWARM_CONSOLE_TOKEN` も使わない。リポジトリは public）。`Accept: application/vnd.github+json`、`User-Agent: swarm-console/<版>`。タイムアウト 10 秒、本文は 1 MiB まで。リダイレクトは `api.github.com` の外へ追わない |
| FR-UPD-2 | `/releases/latest` はドラフトとプレリリースを返さないので、通知の対象は正式版だけ（プレリリースは通知しない） |
| FR-UPD-3 | 版の形は `vX.Y.Z`（正規表現 `^v(0\|[1-9][0-9]*)\.(0\|[1-9][0-9]*)\.(0\|[1-9][0-9]*)$`）。Issue #8 が付けるタグと同じ。最新側の `tag_name` がこの形に合わなければ `release_invalid`。比較は `golang.org/x/mod/semver`（自前で解析しない）。現在の版が `dev`・`vX.Y.Z` でない（ビルド時に版を入れていない）ときは通信せず `dev_build`。現在の版がプレリリース形（`v1.2.0-rc.1`）でも、比較は semver どおり（`v1.2.0` のほうが新しい） |
| FR-UPD-4 | 結果は 3 つに区別する：`up_to_date`（最新版以上）・`update_available`（最新版が現在より新しい）・`unknown`（確認できない。`reason` 付き）。`unknown` の `reason`：`network`（接続失敗・タイムアウト・5xx）・`rate_limit`（429、または 403 で `X-RateLimit-Remaining: 0` か `Retry-After`）・`access`（401・その他の 403）・`no_release`（404。リリース未公開、またはリポジトリが見えない）・`release_invalid`（JSON が読めない・版の形に合わない）・`dev_build` |
| FR-UPD-5 | 確認の失敗は `reason` だけを保持・表示する。URL・ヘッダー・応答本文・リリースノート・生のエラー文面は、ログ・応答・メモに出さない |
| FR-UPD-6 | `swarm-console version`（引数なし）は今までどおり版を 1 行出すだけで通信しない。`swarm-console version --check` は 1 回だけ確認する（バイナリは置き換えない）。標準出力：1 行目に現在の版、2 行目に結果（§12.2）。終了コード：`up_to_date` 0、`update_available` 10、`unknown` 1。`config.toml` を読まず、`update_check` が `false` でも動く（手動の確認） |
| FR-UPD-7 | `serve` は `update_check`（既定 `true`）が `true` のとき、起動の 1 分後に 1 回確認し、以後 `update_check_interval`（既定 24 時間、下限 1 時間）ごとに確認する。結果が `update_available` で `update_auto`（FR-UPD-19）が `true` なら、続けて自己更新（§12.4）に進む。待ちは `clock` を通す（固定時間の `sleep` を使わない）。ツールの呼び出しでは通信しない。確認の結果はメモリだけに持ち、再起動で失ってよい（NFR-AVL-2） |
| FR-UPD-8 | 確認・ダウンロード・入れ替えが失敗・遅延・拒否されても `serve` は動き続ける（入れ替えが成功して自分で終了するとき、FR-UPD-16 を除く）。確認と自己更新は、バックグラウンドの 1 本の goroutine で、起動・ツールの応答・終了（SIGTERM から 10 秒。NFR-AVL-7）を待たせない。SIGTERM を受けたら通信を中断し、書きかけの一時ファイルを消す。失敗後の再試行は次の周期まで行わない。ログ：新しい版を初めて見つけたとき 1 回（同じ版では出さない）、失敗は `reason` が前回と変わったときだけ警告 |
| FR-UPD-9 | `host_status` に `update` を足す（`/report` `/ops` の両方。読み取りのみ）。確認の結果に加えて、自己更新の状況 `apply`（FR-UPD-20）も返す。API 仕様 §5.3。`serve` の確認結果を返すだけで、通信はしない。`report`（markdown）には載せない（AC-1 の 1 文字一致・NFR-PERF-3 の文字量に影響させない） |
| FR-UPD-10 | 確認の設定は `config.toml` の `update_check`（bool、既定 `true`）と `update_check_interval`（期間、既定 `24h`、`1h` 以上）。自己更新の設定は `update_auto`（FR-UPD-19）。いずれも未知キー拒否の対象に加える（ER §5） |

### 12.2 `version --check` の表示

| 結果 | 2 行目 | 終了コード |
|------|--------|-----------|
| `up_to_date` | `最新です（最新版 v0.1.5）` | 0 |
| `update_available` | `新しい版があります: v0.1.5（https://github.com/ousiassllc/swarm-console/releases/tag/v0.1.5）` | 10 |
| `unknown`（`network`） | `確認できませんでした: GitHub に接続できません` | 1 |
| `unknown`（`rate_limit`） | `確認できませんでした: GitHub の取得回数の上限に達しました。しばらくしてからやり直してください` | 1 |
| `unknown`（`access`） | `確認できませんでした: GitHub が取得を拒否しました` | 1 |
| `unknown`（`no_release`） | `確認できませんでした: 公開されたリリースがありません` | 1 |
| `unknown`（`release_invalid`） | `確認できませんでした: 最新リリースの版の形が不正です` | 1 |
| `unknown`（`dev_build`） | `確認しません: この実行ファイルにはリリースの版が入っていません（dev）` | 1 |

URL は最新版が FR-UPD-3 の形に合うときだけ、その版から組み立てる（応答の URL をそのまま出さない）。

### 12.3 通信と安全

- 外向きの通信は、確認の `api.github.com` への GET 1 本と、自己更新のダウンロード（`github.com/ousiassllc/swarm-console/releases/download/<タグ>/…` とそのリダイレクト先。FR-UPD-13）だけ。待ち受けの方針（NFR-SEC-1・2: tailnet の IP だけ、インターネットに出さない）は変えない（NFR-SEC-15）。
- `host_status.update.latest` は FR-UPD-3 の形に合うときだけ出す（GitHub が返した文字列をそのまま MCP の応答に載せない）。ダウンロードの URL も、応答の URL ではなく、検証済みの版から組み立てる。
- バイナリの置き場への書き込みは `~/bin/swarm-console/` の決まったファイルだけ（FR-UPD-15。NFR-SEC-14・16）。新しいバイナリは入れ替える前に実行しない（NFR-SEC-7）。

### 12.4 自己更新の要件

`update_available` を見つけた `serve` が、次の順で自分を入れ替える。

| ID | 要件 |
|----|------|
| FR-UPD-11 | 方式は自己更新。`serve` が確認で `update_available` を見つけ、`update_auto`（既定 `true`）が `true` なら、最新版のバイナリをダウンロードして検証し、動いているバイナリと入れ替え、自分で正常終了して systemd に立ち上げ直してもらう。きっかけは `serve` の自動だけで、運用者が打つ `update` サブコマンドは作らない（P-40・P-46）。`update_auto = false` なら確認だけで、入れ替えは従来の手順（新バイナリを置く → `systemctl --user restart swarm-console`）。`version --check` は入れ替えない |
| FR-UPD-12 | 次を全部満たすときだけ入れ替える。満たさないときは入れ替えず、`apply` を `failed`（FR-UPD-18）か `blocked`（4）にする：(1) `runtime.GOOS`/`GOARCH` が linux/amd64（違えば `unsupported_platform`）(2) 動いているバイナリのパス（`os.Executable()`）が `$HOME/bin/swarm-console/swarm-console` と同じで、通常ファイル（シンボリックリンクでない）、そのフォルダに書ける（違えば `not_managed`）(3) 最新版が現在の版より semver で新しい（ダウングレードしない）(4) 最新版が `swarm-console.update-failed` に記録された版より新しい（戻した版とそれ以下は入れ替えない。FR-UPD-17） |
| FR-UPD-13 | ダウンロード先の URL は、FR-UPD-3 に合う `tag_name`（以下 `<tag>`）から組み立てる：バイナリ `https://github.com/ousiassllc/swarm-console/releases/download/<tag>/swarm-console_<tag>_linux_amd64`、チェックサム `…/download/<tag>/SHA256SUMS`。API 応答の `assets[]`・`browser_download_url` は使わない。認証なし（トークンを送らない）。`https` だけ。リダイレクトは `github.com`・`objects.githubusercontent.com`・`release-assets.githubusercontent.com`（GitHub がリリースの実体を置くホスト）の `https` だけ、最大 5 回追う。接続 10 秒・全体 5 分。`SHA256SUMS` は 64 KiB まで、バイナリは 64 MiB まで（超えたら中断）。404 は `asset_missing`、そのほかの失敗は `download` |
| FR-UPD-14 | 検証は `SHA256SUMS` のハッシュだけ（署名は付けない。限界は §12.5）。`SHA256SUMS` を先に取り、バイナリは書き込みながら SHA-256 を計算する。`SHA256SUMS` は `sha256sum` の出力どおり（1 行 `<64 桁の小文字 16 進>␣␣<ファイル名>`）で、ファイル名が `swarm-console_<tag>_linux_amd64` に一致する行がちょうど 1 行あり、ハッシュが 64 桁の小文字 16 進のとき、それと比べる。行が無い・複数ある・形式が違う・ハッシュが違う・サイズが 0 は `checksum` で、入れ替えない |
| FR-UPD-15 | 入れ替えは `~/bin/swarm-console/` の次の 5 つのファイルだけを使い、この順に行う：(1) `swarm-console.new`（権限 0755。既存は消してから作る）に書いて fsync。検証が済むまで本体に触れない (2) 既存の `swarm-console.old` を消し、動いている `swarm-console` を `swarm-console.old` にハードリンクで残す（本体の名前は一度も消えない。旧バイナリは 1 世代だけ） (3) 試運転のマーカー `swarm-console.update-pending` を一時ファイル経由の `rename` で書く（`{"from":"v0.1.3","to":"v0.1.5","boots":0}`、権限 600） (4) `swarm-console.new` を `swarm-console` へ `rename`（原子的。ここで初めて本体が新しくなる。動いているプロセスは旧バイナリのまま動く） (5) 自分で正常終了（FR-UPD-16）。(1)〜(4) のどこで失敗しても本体は旧バイナリのまま残る。`.new` とマーカーは消す。使うファイルは `swarm-console`・`swarm-console.new`・`swarm-console.old`・`swarm-console.update-pending`・`swarm-console.update-failed`（FR-UPD-17）だけ |
| FR-UPD-16 | 入れ替えのあと、SIGTERM と同じ正常終了を始める（進行中の操作を最大 10 秒待ち、ペインに半端な入力を残さない。NFR-AVL-7）。終了コードは 0 で、systemd の `Restart=always`・`RestartSec=2` が入れ替わった新しいバイナリを起動する。`systemctl` などのコマンドは実行しない（NFR-SEC-7）。`working` のペインがあっても待たず、すぐ入れ替えて終了する（Issue #7 で決定。P-54）。ペインとエージェントは tmux に残り、メモは原子的書き込み（FR-NOTE-9）で失われない。MCP クライアントは接続が切れるのでつなぎ直す（進行中のツール呼び出しは 10 秒で終わらなければ打ち切られる） |
| FR-UPD-17 | 試運転と自動の戻し。`serve` は起動の最初（設定の読み込みより前）に `swarm-console.update-pending` を調べる。マーカーが無ければ通常どおり。`to` が自分の版でなければ（入れ替えが起きなかった）マーカーを消して通常どおり。`to` が自分の版なら `boots` を 1 増やして書き戻し、増やした値が 3 以上（3 回目の起動）なら、`swarm-console.old` を `swarm-console` へ `rename` で戻し、マーカーを消し、`swarm-console.update-failed`（`{"version":"<to>","at":"<時刻>"}`、権限 600）を書き、ERROR ログ（`from`・`to` だけ）を出して終了コード 1 で終了する（systemd が 2 秒後に旧バイナリを起動する）。試運転の終わり：待ち受けを始めてから 60 秒、プロセスが終了しなければ起動できたとみなしてマーカーを消す（INFO ログ）。それより前に終了（パニック・設定エラー・bind 失敗・SIGTERM を含む）すればマーカーは残る。`.old` が無い・戻せないときは、マーカーを消して通常どおり起動を続け（以後は数えない）、ERROR ログを出す。戻ったあとの旧バイナリは、`.update-failed` の版以下を入れ替えない（`blocked`）。それより新しい版が出れば試す。運用者が `.update-failed` を消せば同じ版を再び試す（P-55） |
| FR-UPD-18 | 入れ替えに至らなかったときの `reason`：`unsupported_platform`・`not_managed`・`asset_missing`・`download`（接続失敗・タイムアウト・5xx・サイズ超過・許可されないリダイレクト）・`checksum`・`write`（一時ファイル・リンク・`rename`・マーカーの書き込みの失敗）。本体は置き換わらない。`reason` だけを保持・表示し、URL・ヘッダー・応答本文・生のエラー文面は出さない（FR-UPD-5）。ログ：入れ替えを始めるとき INFO（`from`・`to`）、失敗は `reason` が前回と変わったときだけ警告 |
| FR-UPD-19 | 自己更新の設定は `config.toml` の `update_auto`（bool、既定 `true`）。`false` なら自己更新だけ止まる（確認と `host_status` は続く）。`update_check` が `false` のときは、`update_auto` に関わらず確認も入れ替えもしない。未知キー拒否の対象に加える（ER §5）。反映は `serve` の再起動（NFR-OPS-3）（P-56） |
| FR-UPD-20 | `host_status.update.apply`（常に付く）：`off`（`update_auto` か `update_check` が `false`）・`idle`（入れ替えの失敗・保留が無い。新しい版が無い・まだ確認していないときのほか、入れ替えの実行中を含む。入れ替えが済めば `serve` は終了するので、`update_available` で `idle` なのは実行中だけ）・`probation`（入れ替え直後の試運転中。`from` 付き）・`failed`（`reason` 付き。`update_available` のまま次の周期で再試行）・`blocked`（戻した版が最新のため入れ替えない。`version` 付き）。API 仕様 §5.3 |

```mermaid
flowchart TD
    S["serve 起動"] --> M{"swarm-console.update-pending があるか"}
    M -->|なし| RUN["通常起動"]
    M -->|あり| T{"to は自分の版か"}
    T -->|いいえ| DEL["マーカーを消す"] --> RUN
    T -->|はい| B["boots を +1 して書き戻す"]
    B --> L{"boots が 3 以上か"}
    L -->|いいえ| PROB["試運転で起動"]
    PROB -->|"待ち受け開始から 60 秒生きた"| OK["マーカーを消す（確定）"]
    PROB -->|"60 秒前に終了"| AGAIN["systemd が再起動 → 次の起動で boots が増える"]
    L -->|はい| OLD{"swarm-console.old があるか"}
    OLD -->|あり| RB["old を swarm-console へ rename ・マーカーを消す・update-failed を書く → 終了コード 1"]
    OLD -->|なし| NOOLD["マーカーを消して通常起動（ERROR ログ）"] --> RUN
    RB --> SD["systemd が 2 秒後に旧バイナリを起動"]
```

### 12.5 リスクと限界

検証がハッシュだけで、きっかけが自動、という組み合わせを運用者が選んだ（Issue #7）。次を承知のうえで採る。

| 項目 | 内容 |
|------|------|
| 検出できないもの | `SHA256SUMS` はバイナリと同じリリース・同じ配信元にある。リポジトリやリリースが乗っ取られて差し替えられると、バイナリも `SHA256SUMS` も差し替えられ、検出できない。検出できるのは、転送中の破損・途中切れ・取り違えだけ |
| 影響 | 入れ替わったバイナリは、運用者の操作なしに、すべてのペインへ入力を送れる権限で動く。常駐してエージェントを操作するサーバーなので、乗っ取りはそのまま全ペイン操作の乗っ取りになる |
| 抑えにしているもの | HTTPS のみ・配信元ホストの許可リスト・トークンを送らない・書き込み先を固定 5 ファイルに限る・入れ替え前に新バイナリを実行しない・ダウングレードしない・`update_auto = false` で止められる・リリースは Issue #8 の CI だけが作る |
| 自動で戻せない場合 | 新バイナリがコードを 1 行も実行できずに落ちる（実行形式の不正など）と、`boots` を数えられず自動で戻らない。linux/amd64 の静的ビルドで、ハッシュが CI の成果物と一致する前提では起こりにくい。この場合は運用者が `mv ~/bin/swarm-console/swarm-console.old ~/bin/swarm-console/swarm-console` で戻して `systemctl --user restart swarm-console` する（再起動を繰り返して systemd が起動を諦めたときは、先に `systemctl --user reset-failed swarm-console`） |
| 署名 | 付けない（鍵の用意・秘密の保管・`release.yml` の改訂が要る）。付けるなら別 Issue で FR-UPD-14 を改訂する |

### 12.6 運用者の操作

| やりたいこと | 方法 |
|-------------|------|
| 自動更新を止める | `config.toml` に `update_auto = false` → `systemctl --user restart swarm-console`。確認だけが続き、入れ替えは手で行う |
| 更新の状況を見る | `host_status` の `update`（`status`・`apply`）、`swarm-console version --check`、`journalctl --user -u swarm-console` |
| 手で更新する（`update_auto = false` のとき） | 新バイナリを `~/bin/swarm-console/swarm-console` に置く → `systemctl --user restart swarm-console` → `host_status` で確認 |
| 戻された版を再び試す | `~/bin/swarm-console/swarm-console.update-failed` を消す（次の確認の周期で再び試す） |
| 手で前の版に戻す | `swarm-console.old` を `swarm-console` に戻して再起動（`update_auto = false` にしておかないと、次の確認で再び最新版へ入れ替わる） |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-31 | 新規作成 | 初版 |
| 1.1 | 2026-11-01 | UC9 と §11 対話型の設定（FR-SET）を追加 | Issue #5 |
| 1.2 | 2026-11-01 | UC10 と §12 新しい版の確認（FR-UPD）を追加。更新の方式（確認だけ／自己更新）は未決として §12.4 に併記 | Issue #7 |
| 1.3 | 2026-11-01 | 更新の方式を自己更新で確定し、§12.4 を自己更新の要件（FR-UPD-11〜20）に書き換え、§12.5 リスクと限界・§12.6 運用者の操作を追加。UC10・§12.1 の FR-UPD-6〜10・§12.3 を自己更新に合わせて改訂。検証はハッシュのみ・きっかけは `serve` の自動・再起動は自分で正常終了して systemd に任せる・作業中でも更新・起動失敗は `.old` から自動で戻す・`update_auto` を追加・置き場は `~/bin/swarm-console/` に固定・アセット名は版・OS・arch 入り | Issue #7（自己更新で確定） |
