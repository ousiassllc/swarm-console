# API 仕様：レポート・指示・prompts・resources・上限

[API 仕様](endpoints.md) の続き。§5 ツールのうち、§5.5 `report` と §5.6 `dispatch`、さらに §6 prompts と resources、§7 上限一覧を定める。

改訂履歴は [API 仕様（本書）](endpoints.md#改訂履歴) にまとめてある。

## 5. ツール（続き）

### 5.5 `report`

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `format` | string | 任意 | `markdown`（既定）・`json` |
| `pane` | string | 任意 | 指定すると 1 ペインの詳しい版 |
| `lang` | string | 任意 | `ja`（既定、`default_lang` で変更）・`en` |

- `markdown`：`content[0].text` に機能要件 §5.3 の markdown 本文を入れる（他のキーは足さない）。ツールの説明に「返った markdown に手を加えずそのまま表示する」と書く。
- `json`：`panes_status` と同じ構造に、要対応の一覧を足す。言語に依存しない。

```json
{
  "at": "2026-10-31T22:10:05+09:00", "host": {"load1": 98.2, "mem_free_gb": 41.3}, "layout": {"rows": 2, "cols": [4, 4]},
  "panes": ["... panes_status と同じ"],
  "attention": [
    {"kind": "asking", "pane": "T2", "detail": "(2) の Go での範囲をどうしますか？"},
    {"kind": "stalled", "pane": "B3", "detail": "20m"},
    {"kind": "report_waiting", "pane": "T1", "detail": "/tmp/swarm-console/reports/T1.md"}
  ],
  "warnings": []
}
```

同じスナップショットなら、クライアントによらず同じ出力（FR-REP-7）。

### 5.6 `dispatch`（`/ops` のみ）

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `pane` | string | 必須 | `shell` または `idle` のペイン |
| `agent` | string | 必須 | `omp`・`claude`・`codex`。`idle` のペインで実際のエージェントと違えば `invalid_args` |
| `cwd` | string | `shell` のとき必須 | `pane_start` と同じ規則。`idle` のペインでは無視（省略可） |
| `title` | string | `shell` のとき必須 | `pane_start` と同じ規則 |
| `command` | string | 任意 | 1 行目のスラッシュコマンド（例：`/issue-sweep #982 --single-pr --base main`）。改行・制御文字不可 |
| `task` | string | 必須 | やることの本文 |
| `profile` | string | 必須 | `profiles.toml` のプロファイル名（`sweep`・`fix`・`investigate` ほか） |
| `extra_rules` | string[] | 任意 | 追加の条件（各 1 行） |
| `note` | object | 任意 | メモ（`note_set` の `set` と同じ項目）。一緒に保存する |

```json
{
  "ok": true, "state": "working", "accepted": true,
  "started": true,
  "sent": "/issue-sweep #982 --single-pr --base main\n\n## やること\n...\n\n## 条件\n- コミットは必ず ...\n",
  "note_saved": true
}
```

#### 指示文の組み立て

```
<command>                       ← 指定があれば 1 行目（なければ省略）

## やること
<task>

## 条件
- <共通の条件 1>
...
- <共通の条件 8>
- <プロファイルの条件>
- <extra_rules の各行>
```

- 共通の条件 → プロファイルの条件 → `extra_rules` の順。`{report_path}` は最終報告ファイルの実パス（`/tmp/swarm-console/reports/<位置>.md`）に置換。
- 見出し（`## やること`・`## 条件`）と `profiles.toml` の条件文以外の文は足さない。人の名前・経緯は入れない。
- 組み立て後の全文は 8000 字以内。`forbidden_control_char`（`\n`・`\t` 以外）・`text_too_long`・`secret_detected`（`note`）はここで検出し、何も送らない。

#### 手順と拒否

| ペインの状態 | 動き |
|-------------|------|
| `shell` | `pane_start`（`cwd`・`title`）→ `idle` を待つ → 指示文を送る。`started: true` |
| `idle` | そのまま指示文を送る。`started: false` |
| `working`・`asking`・`stalled`・`unknown` | `invalid_state`（何も送らない） |

送る前に、そのペインの古い最終報告ファイルを `archive/` へ移す。送ったあと、受理を確認し、`note` があれば保存して `note_saved` に入れる。

## 6. prompts と resources

### 6.1 prompts

| 名前 | 窓口 | 引数 | 内容 |
|------|------|------|------|
| `report` | `/report`・`/ops` | `lang`（任意） | 「`report` ツールを呼び、返った markdown を手を加えずそのまま表示する」というユーザーメッセージ |
| `dispatch` | `/ops` | `pane`・`agent`・`cwd`・`title`・`command`・`task`・`profile`・`extra_rules`（`profile` と `task` は必須） | 「この引数のまま `dispatch` ツールを呼び、返った `sent` と結果を報告する」というユーザーメッセージ |

プロンプトの文面はサーバー側で固定（クライアントによらず同じ）。

### 6.2 resources

| URI | MIME | 内容 |
|-----|------|------|
| `swarm://report` | `text/markdown` | `report`（markdown、既定言語）と同じ |
| `swarm://pane/<位置>` | `text/markdown` | `report` の `pane` 指定（1 ペインの詳しい版） |
| `swarm://notes/<位置>` | `application/json` | `note_get` と同じ |

- `<位置>` は位置の名前または tmux ID。ID の `%` は URI では `%25` にする（`swarm://pane/%2512`）。`resources/list` は現在のペインぶんの URI を列挙し、`resources/templates/list` にテンプレート（`swarm://pane/{pos}`・`swarm://notes/{pos}`）を返す。
- 解決できない位置は `pane_not_found`（resources は JSON-RPC のエラーコード `-32002` ＋ 同じ本文）。

## 7. 上限一覧

| 対象 | 上限 | 超えたとき |
|------|------|-----------|
| `send_text` の `text`／`file` の中身／`dispatch` の組み立て後の全文／`ask_pick` の `text` | 8000 字 | `text_too_long` |
| `pane_tail` の `lines` | 200 行（既定 30） | `invalid_args` |
| `pane_tail` の本文 | 20,000 字・1 行 500 字 | 切り詰め ＋ `truncated: true` |
| `report_read` の本文 | 20,000 字 | 切り詰め ＋ `truncated: true` |
| `ask_card` の `question` | 1,000 字、選択肢 `label` 300 字、選択肢 20 個 | 切り詰め ＋ `truncated: true` |
| `panes_status` の `activity` | 120 字 | 切り詰め ＋ `truncated: true` |
| メモの文字列項目 | 2,000 字（`memo` 4,000 字）、配列 50 件 | `invalid_args` |
| `note_log` の 1 行 | 500 字 | `invalid_args` |
| `title` | 80 字 | `invalid_args` |
| リクエスト本文 | 256 KiB | 413 |
