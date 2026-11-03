# API 仕様：ペインを見る・操作するツール

[API 仕様](endpoints.md) の続き。§5 ツールのうち、§5.1 見る（`panes_status`・`pane_tail`・`ask_card`・`report_read`）と §5.2 操作する（`ask_pick`・`send_text`・`send_escape`・`pane_start`・`pane_close`・`file_get`）を定める。

改訂履歴は [API 仕様（本書）](endpoints.md#改訂履歴) にまとめてある。

## 5. ツール

### 5.1 見る

`session`（任意）を取るツールの、省略の規則・ペインの解決・応答の絞り方は [API 仕様 §4.1](endpoints.md#41-session-引数と許可セッション) にまとめてある。下の表の `session` はどれも同じ意味。

#### `panes_status`

対象セッションの全ペインを 1 回で返す。

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `session` | string | 任意 | 対象のセッション。省略は全セッションではなく、§4.1 の規則（許可が 1 つならそれ、そうでなければ `session_required`）。`sessions` にない名前は `invalid_args`、許可外は `session_forbidden` |

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
      "note": {"from": "agent-b", "origin": "agent", "started_by": "かな", "repo": "ousiassllc/funid", "items": ["#982"], "next": "完了したら #983 を入れる"}
    }
  ],
  "warnings": []
}
```

- `agent` は `omp`・`claude`・`codex`・`null`。`activity` はエージェントがいま何をしているかの 1 行（取れなければ `null`。120 字で切ったペインにだけ、`note` のあとに `"truncated": true` が付く。切っていなければキーごと出さない）。`report` の JSON の `panes[]` も同じ。
- `note` はメモの要約（`from`・`origin`・`started_by`・`repo`・`items`・`next` だけ。空の項目は出さない）。`origin` は `user`・`agent`（FR-NOTE-2）。メモがなければ `null`。
- `warnings[]` は `{"code": "layout_changed", "message": "..."}` の配列（コード：`layout_changed`・`tmux_unavailable`・`session_missing`・`pane_unreadable`）。
- 応答は 1 つのセッションに絞る（§4.1）。`pos` はそのセッション内の位置（接頭辞なし。3 列×2 段なら `T1`〜`T3`・`B1`〜`B3`）、`layout` はそのセッションのペインから計算し、`host` は全体の負荷のまま。`warnings` はそのセッションの分と全体の警告（`tmux_unavailable`）だけ。

#### `pane_tail`

1 ペインの最後の n 行を、枠線・TODO 表示・ステータス行を取り除いた本文だけで返す。

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `pane` | string | 必須 | |
| `session` | string | 任意 | §4.1 |
| `lines` | int | 任意 | 既定 30、1〜200 |
| `raw` | bool | 任意 | `true` なら飾りも残す |

```json
{"pane": "B4", "lines": 30, "text": "...\n...", "truncated": false}
```

`unknown` のペインは飾りの除去をせず `raw` 相当で返す。`pane` は対象のセッション内の位置（接頭辞なし）。

#### `ask_card`

| 引数 | 型 | 必須 |
|------|-----|------|
| `pane` | string | 必須 |
| `session` | string | 任意 |

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
| `session` | string | 任意 |

```json
{"exists": true, "path": "/tmp/swarm-console/reports/B4.md", "updated_at": "2026-10-31T22:01:00+09:00", "text": "...", "truncated": false}
```

ファイルがなければ `{"exists": false}`。`path` のファイル名は、セッション内の位置ではなく全体の位置から付く（既定セッションは `T1.md`、それ以外の `1:T1` は `1_T1.md`。ER §7）ので、別のセッションのファイルは衝突しない。

### 5.2 操作する（`/ops` のみ）

共通の返り値：`{"ok": bool, "state": "...", ...}`。操作後の状態はスナップショットを取り直した結果。

#### `ask_pick`

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `pane` | string | 必須 | |
| `session` | string | 任意 | §4.1 |
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
| `session` | string | 任意 | §4.1 |
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
| `session` | string | 任意 | §4.1 |
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
| `session` | string | 任意 | §4.1 |
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
| `session` | string | 任意 |

```json
{"ok": true, "state": "shell", "steps": ["/exit", "cd ~", "clear", "title"], "note_archived": true}
```

終了コマンド → `cd ~` → `clear` → タイトル消し、を 1 つずつ「文字列 → 1 秒 → Enter → 1 秒」で送る（`shell` のペインは終了コマンドを飛ばす）。終了コマンドのあとは、ペインの状態が `shell` に戻るまで、スナップショットを 500 ms ごとに取り直して待ってから `cd ~` 以降を送る（終了が遅れる `omp` に `cd ~` が入力されないようにする）。`shell` に戻ったかは `panes_status` と同じ状態の判定（前面コマンド・プロセス木 `/proc`、取れなければ画面で補う）で見る。`/proc` が読めないときは、前面コマンドが shell なら戻ったとみなし、終了後も画面に残ったエージェントの表示では判定しない（正常に終了したのに `exit_timeout` にならない）。待つ上限は `accept_timeout`（既定 10 秒）。上限までに戻らなければ、`cd ~`・`clear`・タイトル消しは送らず、エラー `exit_timeout`（`details.pane`・`details.state`）だけを返す。結果の JSON（`ok`・`state`・`steps`・`note_archived`）は返らない。メモは履歴へ移さず、最終報告ファイルも `archive/` へ移さない。終了コマンドはすでに送ってあるので、呼び出し側は `pane_close` を呼び直してよい（そのときペインが `shell` なら終了コマンドは飛ばす）。`shell` に戻ったあとは、最後に画面が `~` のプロンプトだけになったかを確かめ、なっていなければ `ok: false`。確かめられたとき（`ok: true`）だけ、ペインのメモを履歴へ移し（`note_archived: true`）、最終報告ファイルを `archive/` へ移す。`idle`・`shell` 以外は `invalid_state`。

「`~` のプロンプトだけ」には、fish・starship のように枠で囲んだ複数行のプロンプトも含む。含むのは次を全部満たすものだけ：3 行以内、先頭行が `╭┌╔┏` で始まり `~` を含む、末尾行が `╰└╚┗` で始まりプロンプトの記号（`$ # % > ❯ ➜ »`）で終わる、間の行は無いか縦線（`│├┃║`）だけ。枠の中に出力が残っていれば片付いていない。右端まで閉じた箱（行末が `╮╯` など）もプロンプトとみなさない。

#### `file_get`（`/ops` のみ）

ompが撮ったスクショ・動画をサーバーから取るツール（scp 不要）。ペインには何も送らない（読むだけ）。`/report` には出さない（`ops` スコープのトークンでも `/report` では `unknown_tool`）。

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `path` | string | 必須 | サーバー上のファイルの絶対パス 1 つ。`session` は取らない |

```json
{"path": "/mnt/nvme/ousiassllc/funid/shot.png", "mime": "image/png", "size": 183204, "sha256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", "data_base64": "iVBORw0KGgo..."}
```

キー順は `path`・`mime`・`size`・`sha256`・`data_base64`。`path` は実パス（シンボリックリンクを解決したもの）、`size` はバイト数、`sha256` は元のバイト列の SHA-256（小文字の hex。サーバー上のファイルをそのままハッシュした値と一致する）、`data_base64` は標準の base64（パディングあり）。`mime` は拡張子ではなく中身（先頭バイト）から決める。

**許可リスト**（取れるのは、この下の実パスだけ。P-91）

- 呼び出し元のトークンが触れるセッション（[§4.1](endpoints.md#41-session-引数と許可セッション)）のペインの、現在の `cwd` の下
- レポート置き場 `report_dir`（既定 `/tmp/swarm-console/reports`）の下
- `config.toml` の `file_get_roots` に足した根の下（既定は空）

判定は `filepath.EvalSymlinks` で実パスにしたうえで、同じく実パスにした根の配下かどうかで行う（根自体がシンボリックリンクでもよい。`/a/b` と `/a/bc` は別。文字列の接頭辞では比べない）。`denied_paths` の一致・配下も断る。

**判定の順序と結果**（上から最初に当たったもの）

| 条件 | エラー（`details.reason`） |
|------|---------------------------|
| `path` が空・制御文字を含む・引数の型が違う | `invalid_args` |
| 相対パス | `path_forbidden`（`not_absolute`） |
| `..` の成分を含む（結果が許可リストの中に戻るものも断る） | `path_forbidden`（`dotdot`） |
| ファイル名が秘密のもの：`.env`・`.env.*`・`*.pem`・`*.key`・`id_*`・`config.toml`・`tokens.toml`・`profiles.toml`（大文字小文字は区別しない。入力の名前・実パスの名前の両方で見る。例 `id_rsa.png`、無害な名前のリンクで実体が `secret.key`） | `path_forbidden`（`secret`） |
| 実パスが許可リストの外 | `path_forbidden`（`outside_roots`）。存在しなくても同じ（外のパスの存在を漏らさない） |
| 実パスが `denied_paths` の一致・配下 | `path_forbidden`（`denied`） |
| 実パスが tokens.toml・profiles.toml・notes.json の実パスと一致（名前が普通でも） | `path_forbidden`（`secret`） |
| 許可リストの中だが存在しない | `not_found` |
| 実パスの拡張子（小文字）が `png`・`jpg`・`jpeg`・`webp`・`gif`・`mp4` 以外 | `unsupported_type`（`extension`） |
| 通常ファイルでない（ディレクトリ・FIFO・デバイス。FIFO で固まらないよう非ブロッキングで開く） | `unsupported_type`（`not_regular`） |
| サイズが上限（`file_get_max_bytes`、既定 20 MiB = 20971520 バイト）を超える。ちょうど上限は通る | `too_large`（`details.max`） |
| 先頭バイトが拡張子の形式と合わない（PNG：`\x89PNG\r\n\x1a\n`、JPEG：`FF D8 FF`、WebP：`RIFF????WEBP`、GIF：`GIF87a`・`GIF89a`、MP4：4 バイト目から `ftyp`。`.jpg`・`.jpeg` は JPEG） | `unsupported_type`（`content`） |

サイズは読む前に `stat` で判定し、読み込みも上限＋1 バイトで打ち切る（確認のあとに大きくされても上限を超えて読まない。そのときも `too_large`）。シンボリックリンクは解決した実パスを `O_NOFOLLOW` で開く（最後の成分の差し替えを拒否）。開いたあとに `/proc/self/fd/<fd>` の実パスを読み、確認した実パスと違えば（確認後に途中のディレクトリがリンクへ差し替えられた場合）`path_forbidden`（`unreadable`）で断り、中身は読まない。

**監査ログ**：成功・拒否のどちらも 1 行に、`client`（トークン名）・`tool: file_get`・`path`（解決できたときは実パス、断った場合は入力のまま。512 字で切る）・`size`（バイト。ファイルを開けたときだけ。上限超えは実際のサイズ）・`code`（`ok` またはエラーコード）を残す。`data_base64`・ファイルの中身は残さない（NFR-SEC-12）。

#### `dispatch` → §5.6
