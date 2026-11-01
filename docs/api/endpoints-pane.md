# API 仕様：ペインを見る・操作するツール

[API 仕様](endpoints.md) の続き。§5 ツールのうち、§5.1 見る（`panes_status`・`pane_tail`・`ask_card`・`report_read`）と §5.2 操作する（`ask_pick`・`send_text`・`send_escape`・`pane_start`・`pane_close`）を定める。

改訂履歴は [API 仕様（本書）](endpoints.md#改訂履歴) にまとめてある。

## 5. ツール

### 5.1 見る

#### `panes_status`

対象セッションの全ペインを 1 回で返す。

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `session` | string | 任意 | 省略で全セッション。`sessions` にないセッション名は `invalid_args` |

```json
{
  "at": "2026-10-31T22:10:05+09:00",
  "host": {"load1": 98.2, "mem_free_gb": 41.3},
  "layout": {"rows": 2, "cols": [4, 4]},
  "panes": [
    {
      "pos": "B4", "id": "%12", "agent": "omp", "state": "working",
      "title": "agent: funid #982", "cwd": "/mnt/nvme/ousiassllc/funid-sweep-982",
      "elapsed": "42m", "activity": "Running go test ./...",
      "changed_ago": "20s",
      "note": {"from": "agent-b", "repo": "ousiassllc/funid", "items": ["#982"], "next": "完了したら #983 を入れる"}
    }
  ],
  "warnings": []
}
```

- `agent` は `omp`・`claude`・`codex`・`null`。`activity` はエージェントがいま何をしているかの 1 行（取れなければ `null`）。
- `note` はメモの要約（`from`・`repo`・`items`・`next` だけ）。メモがなければ `null`。
- `warnings[]` は `{"code": "layout_changed", "message": "..."}` の配列（コード：`layout_changed`・`tmux_unavailable`・`session_missing`・`pane_unreadable`）。

#### `pane_tail`

1 ペインの最後の n 行を、枠線・TODO 表示・ステータス行を取り除いた本文だけで返す。

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `pane` | string | 必須 | |
| `lines` | int | 任意 | 既定 30、1〜200 |
| `raw` | bool | 任意 | `true` なら飾りも残す |

```json
{"pane": "B4", "lines": 30, "text": "...\n...", "truncated": false}
```

`unknown` のペインは飾りの除去をせず `raw` 相当で返す。

#### `ask_card`

| 引数 | 型 | 必須 |
|------|-----|------|
| `pane` | string | 必須 |

```json
{"asking": true, "question": "(2) の Go での範囲をどうしますか？",
 "options": [{"n": 1, "label": "TS 仕様どおり", "recommended": true}, {"n": 2, "label": "ライブラリ関数として拡張"}],
 "cursor": 1, "allows_custom": true}
```

`asking` でなければ `{"asking": false}`。`unknown` のペインは `unsupported_agent`。

#### `report_read`

| 引数 | 型 | 必須 |
|------|-----|------|
| `pane` | string | 必須 |

```json
{"exists": true, "path": "/tmp/swarm-console/reports/B4.md", "updated_at": "2026-10-31T22:01:00+09:00", "text": "...", "truncated": false}
```

ファイルがなければ `{"exists": false}`。

### 5.2 操作する（`/ops` のみ）

共通の返り値：`{"ok": bool, "state": "...", ...}`。操作後の状態はスナップショットを取り直した結果。

#### `ask_pick`

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `pane` | string | 必須 | |
| `n` | int | `text` がなければ必須 | 選ぶ番号（1 始まり） |
| `text` | string | 任意 | 「その他」に打つ文字。`n` と同時指定は不可（P-10）。8000 字まで、制御文字は §5.2 `send_text` と同じ規則 |

```json
{"ok": true, "state": "working", "picked": {"n": 1, "label": "TS 仕様どおり"}, "card_closed": true}
```

キーを送ったあとは、画面にカーソルの移動が映るまで（1 試行あたり最大 1 秒）画面を取り直して確かめる。映る前に次のキーを送らない。カーソルが 3 回の試行で合わなければ（最大 3 秒）`cursor_mismatch`（Enter は送らない）。カードが閉じず `working` にもならなければ `{"ok": false, "state": "asking", "card_closed": false}`。

#### `send_text`

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `pane` | string | 必須 | |
| `text` | string | `file` がなければ必須 | 8000 字まで。許可する制御文字は `\n`・`\t` のみ |
| `file` | string | `text` がなければ必須 | サーバー上のファイル。`file_roots` の下、UTF-8 で 8000 字まで。`text` と同時指定は不可 |

```json
{"ok": true, "state": "working", "accepted": true, "chars": 412}
```

tmux の bracketed paste で貼り付け、1 秒待って Enter。入力欄が空になって `working` になったら `accepted: true`。`accept_timeout` 内に受理されなければ `{"ok": false, "accepted": false, "state": "idle"}`。Ctrl-C などの特別なキーは送れない。

#### `send_escape`

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `pane` | string | 必須 | |
| `confirm` | bool | 必須 | `true` でなければ `confirm_required` |

```json
{"ok": true, "state": "idle", "interrupted": true}
```

- ツールの説明に「**オーナーの OK があるときだけ使う**」と書く。
- 実行は `note_log` に `Esc を送った（中断）` として自動で残る。
- 送って状態が変わらなければ `{"ok": false, ...}`。

#### `pane_start`

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `pane` | string | 必須 | `shell` のペインのみ |
| `agent` | string | 必須 | `omp`・`claude`・`codex` |
| `cwd` | string | 必須 | 許可したフォルダの下の既存ディレクトリ |
| `title` | string | 必須 | 80 字まで、制御文字なし。タイトルは `agent: <title>` になる |

```json
{"ok": true, "state": "idle", "agent": "omp", "cwd": "/mnt/nvme/ousiassllc/funid-sweep-982", "title": "agent: funid #982"}
```

`cd <cwd>` → エージェントの起動コマンド、の順に送り、`start_timeout` 以内に `idle` になったかを返す（ならなければ `ok: false`）。`cwd` はシェルに渡す前にクォートし、制御文字・`'` を含むものは `invalid_args`。

#### `pane_close`

| 引数 | 型 | 必須 |
|------|-----|------|
| `pane` | string | 必須 |

```json
{"ok": true, "state": "shell", "steps": ["/exit", "cd ~", "clear", "title"], "note_archived": true}
```

終了コマンド → `cd ~` → `clear` → タイトル消し、を 1 つずつ「文字列 → 1 秒 → Enter → 1 秒」で送る（`shell` のペインは終了コマンドを飛ばす）。終了コマンドのあとは、ペインの状態が `shell` に戻るまで、スナップショットを 500 ms ごとに取り直して待ってから `cd ~` 以降を送る（終了が遅れる `omp` に `cd ~` が入力されないようにする）。`shell` に戻ったかは `panes_status` と同じ状態の判定（前面コマンド・プロセス木 `/proc`、取れなければ画面で補う）で見るので、`/proc` が読めることに依る。待つ上限は `accept_timeout`（既定 10 秒）。上限までに戻らなければ、`cd ~`・`clear`・タイトル消しは送らず、エラー `exit_timeout`（`details.pane`・`details.state`）だけを返す。結果の JSON（`ok`・`state`・`steps`・`note_archived`）は返らない。メモは履歴へ移さず、最終報告ファイルも `archive/` へ移さない。終了コマンドはすでに送ってあるので、呼び出し側は `pane_close` を呼び直してよい（そのときペインが `shell` なら終了コマンドは飛ばす）。`shell` に戻ったあとは、最後に画面が `~` のプロンプトだけになったかを確かめ、なっていなければ `ok: false`。確かめられたとき（`ok: true`）だけ、ペインのメモを履歴へ移し（`note_archived: true`）、最終報告ファイルを `archive/` へ移す。`idle`・`shell` 以外は `invalid_state`。

「`~` のプロンプトだけ」には、fish・starship のように枠で囲んだ複数行のプロンプトも含む。含むのは次を全部満たすものだけ：3 行以内、先頭行が `╭┌╔┏` で始まり `~` を含む、末尾行が `╰└╚┗` で始まりプロンプトの記号（`$ # % > ❯ ➜ »`）で終わる、間の行は無いか縦線（`│├┃║`）だけ。枠の中に出力が残っていれば片付いていない。右端まで閉じた箱（行末が `╮╯` など）もプロンプトとみなさない。

#### `dispatch` → §5.6
