# API 仕様：レポート・指示・prompts・resources・上限

[API 仕様](endpoints.md) の続き。§5 ツールのうち、§5.5 `report` と §5.6 `dispatch`、さらに §6 prompts と resources、§7 上限一覧を定める。

改訂履歴は [API 仕様（本書）](endpoints.md#改訂履歴) にまとめてある。

## 5. ツール（続き）

### 5.5 `report`

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `format` | string | 任意 | `markdown`（既定）・`json` |
| `pane` | string | 任意 | 指定すると 1 ペインの詳しい版 |
| `session` | string | 任意 | [API 仕様 §4.1](endpoints.md#41-session-引数と許可セッション)。ペインに関する項目をそのセッションだけに絞る |
| `lang` | string | 任意 | `ja`（既定、`default_lang` で変更）・`en` |

- `markdown`：`content[0].text` に機能要件 §5.3 の markdown 本文を入れる（他のキーは足さない）。ツールの説明に「返った markdown に手を加えずそのまま表示する」と書く。
- `json`：`panes_status` と同じ構造に、要対応の一覧と、足した値（使用状況・メモリ・CPU・スワップ・ディスク・更新確認・ログイン期限）を入れる。言語に依存しない。値は全部スナップショットに入った値（FR-REP-10）。
- `session`：`panes`・`layout`・`attention` のペインの項目・`warnings` を、決まったセッションだけに絞る（`pos` はセッション内の位置。`report_waiting` の `detail` のパスのファイル名は全体の位置から付く。§4.1）。`host`・`usage`・`omp`・`update`・`auth` と、ログインの要対応（`auth_expired`・`auth_expiring`）は絞らず全体の値を入れる。markdown も同じ（ペインの表・要対応のペインの項目・警告だけがそのセッション。見出し・使用状況は全体）。`pane` と一緒に指定したときは、`pane` をそのセッションの中で解決する（§4.1）。許可セッションが 2 つ以上のトークンが `session` を省略すると `session_required`。

```json
{
  "at": "2026-10-31T22:10:05+09:00",
  "host": {
    "load1": 98.2, "mem_free_gb": 41.3,
    "mem": {"available": true, "total_gb": 128.0, "used_gb": 86.7, "used_percent": 68},
    "cpu": {"available": true, "used_percent": 42},
    "swap": {"available": true, "total_gb": 8.0, "used_gb": 0.4, "used_percent": 5},
    "disk": [
      {"mount": "/", "available": true, "free_gb": 120, "total_gb": 450, "used_percent": 73},
      {"mount": "/mnt/nvme", "available": true, "free_gb": 830, "total_gb": 3700, "used_percent": 78}
    ]
  },
  "layout": {"rows": 2, "cols": [4, 4]},
  "panes": ["... panes_status と同じ（elapsed を含む）"],
  "attention": [
    {"kind": "asking", "pane": "T2", "detail": "(2) の Go での範囲をどうしますか？"},
    {"kind": "stalled", "pane": "B3", "detail": "20m"},
    {"kind": "report_waiting", "pane": "T1", "detail": "/tmp/swarm-console/reports/T1.md"},
    {"kind": "auth_expired", "pane": "", "detail": "claude"},
    {"kind": "auth_expiring", "pane": "", "detail": "codex"}
  ],
  "usage": {
    "status": "ok", "reason": null, "fetched_at": "2026-10-31T22:09:30+09:00",
    "claude": [
      {"account": "acct-a", "reason": null,
       "five_hour": {"available": false, "used": null, "unit": null, "status": null, "resets_at": null},
       "seven_day": {"available": true, "used": 48, "unit": "percent", "status": "ok", "resets_at": "2026-11-01T07:00:00+09:00"}},
      {"account": "acct-b", "reason": "without_usage",
       "five_hour": {"available": false, "used": null, "unit": null, "status": null, "resets_at": null},
       "seven_day": {"available": false, "used": null, "unit": null, "status": null, "resets_at": null}}
    ],
    "cursor": [
      {"account": "acct-a", "reason": null, "window": "monthly",
       "models": {"available": true, "used": 0.0, "unit": "percent", "status": "ok", "resets_at": "2026-12-01T09:00:00+09:00"}},
      {"account": "acct-b", "reason": null, "window": "weekly",
       "models": {"available": true, "used": 0.0, "unit": "usd", "status": "ok", "resets_at": "2026-11-02T09:00:00+09:00"}}
    ],
    "codex": [
      {"account": "acct-a", "reason": null,
       "five_hour": {"available": true, "used": 0.0, "unit": "percent", "status": "ok", "resets_at": "2026-11-02T03:09:00+09:00"},
       "seven_day": {"available": true, "used": 2.0, "unit": "percent", "status": "ok", "resets_at": "2026-11-04T09:00:00+09:00"}}
    ]
  },
  "omp": {"status": "up_to_date", "current": "18.14.2", "latest": "18.14.2", "reason": null, "checked_at": "2026-10-31T09:00:00+09:00"},
  "update": {"status": "up_to_date", "current": "v0.1.5", "latest": "v0.1.5", "checked_at": "2026-11-01T12:00:00+09:00", "apply": {"state": "idle"}},
  "auth": {"agents": [
    {"agent": "omp", "status": "unknown", "expires_at": null},
    {"agent": "claude", "status": "expired", "expires_at": null},
    {"agent": "codex", "status": "expiring_soon", "expires_at": "2026-11-01T08:10:00+09:00"}
  ]},
  "warnings": []
}
```

同じスナップショット・同じ `session` なら、クライアントによらず同じ出力（FR-REP-7）。

#### 足した値の形

`host.mem`・`host.cpu`・`host.swap`・`host.disk[]`・`usage`・`omp`・`update` は `report` の JSON と `host_status` で共通（`host_status` では `host.` を付けず、`mem`・`cpu`・`swap`・`disk` として直下に返す。FR-REP-17）。`auth`・`attention[]` は `report` の JSON だけで、`host_status` は返さない（ログイン期限は `agent_auth_status`）。

| キー | 形 | 取れないとき |
|------|-----|-------------|
| `host.mem` | `{available, total_gb, used_gb, used_percent}`（GB は小数 1 位、率は整数）。`used_gb` は `MemTotal − MemAvailable`、`used_percent` は `100 × used ÷ total` の整数。`host.mem_free_gb`（`MemAvailable`）は互換のため今までどおり返す（`used_gb + mem_free_gb ≒ total_gb`）。取り方は [report-values.md §3](../architecture/report-values.md#3-サーバー負荷issue-23) | `available: false`・数値は `null`（`/proc/meminfo` を読めない・項目がない・数値が不正なとき・`MemTotal` が 0 以下のとき・`MemAvailable` が `MemTotal` より大きいとき。メモリが読めなくても `host` の `load1` などほかの値は読めたまま。`host.mem_free_gb` は読めたときはその値、読めないときは `0` なので、読み手は `mem.available` を見る（`panes_status` の `host.mem_free_gb` も同じく 0 になり、そちらには `available` に当たる印がない）。P-95） |
| `host.cpu` | `{available, used_percent}`（整数 0〜100） | `{"available": false, "used_percent": null}`（起動後の最初のサンプルなど。取り方は [report-values.md §3](../architecture/report-values.md#3-サーバー負荷issue-23)） |
| `host.swap` | `{available, total_gb, used_gb, used_percent}`（GB は小数 1 位、率は整数）。スワップ無しは `total_gb: 0`・`used_percent: 0` | `available: false`・数値は `null` |
| `host.disk[]` | 既存の `mount`・`free_gb` に `available`・`total_gb`・`used_percent`（整数）を足す。要素は `disk_mounts` の順 | そのマウントを残し `available: false`・数値は `null` |
| `usage` | 下の表 | `status` と `reason` で示す |
| `omp` | 下の表 | `status: unknown` と `reason` |
| `update` | `host_status.update` と同じ（[§5.3](endpoints-state.md#53-サーバーの状態)） | 同じ |
| `auth`（`report` のみ） | `agent_auth_status` と同じ（`agent`・`status`・`expires_at`）。順は `omp`・`claude`・`codex` | `status: unknown`・`expires_at: null` |
| `attention[]`（`report` のみ） | 既存の 3 キー `kind`・`pane`・`detail`（どれも文字列）のまま。ログインの項目は `pane` を `""`、`detail` をエージェント名（`omp`・`claude`・`codex`）にする。期限の日時は `auth` の同じエージェントの `expires_at` を見る | — |

- `attention[].kind` は `asking`・`stalled`・`report_waiting`・`auth_expired`・`auth_expiring`。並びは、ペインの項目（質問待ち → 止まってそう → 報告待ち、各ペインの並び順）のあとに `auth_expired` → `auth_expiring`（それぞれ `auth.agents` の順）。`unknown` は入れない（FR-REP-16）。
- `pane` を指定した JSON では、`panes` と、ペインの項目（`asking`・`stalled`・`report_waiting`）はそのペインだけに絞る。`auth_expired`・`auth_expiring`・`usage`・`omp`・`update`・`auth`・`host` はペインに属さないので絞らず、全体の版と同じ値を常に入れる（markdown の詳しい版が、見出し・要対応・使用状況を全体の版と同じに出すのと同じ。FR-REP-16）。並びは変わらない（ペインの項目 → ログイン）。
- 既存の `attention[]` のキー・型は変えない（`pane` は常に文字列。ログインの項目だけ空文字列）。

`usage` のキー：

| キー | 内容 |
|------|------|
| `status` | `ok`（最後の取得が成功）・`stale`（最後の取得が失敗、前の成功分を残す）・`unavailable`（成功が一度も無い。アカウントは空）・`pending`（起動後まだ取得していない。アカウントは空）・`disabled`（`usage_check = false`。アカウントは空） |
| `reason` | `stale`・`unavailable` のとき `not_found`・`timeout`・`exec`・`parse`。それ以外は `null` |
| `fetched_at` | 最後に成功した取得の時刻（RFC 3339）。成功が無ければ `null` |
| `claude[]` | `account`・`reason`・`five_hour`・`seven_day`。`omp` が返した Claude のアカウントを全部出す（`account` の昇順） |
| `cursor[]` | `account`・`reason`・`window`（`monthly`・`weekly` など。枠が無ければ `null`）・`models` |
| `codex[]` | `account`・`reason`・`five_hour`・`seven_day` |
| 枠（`five_hour` など） | `available`・`used`（小数 1 位）・`unit`（`percent`・`usd`）・`status`（`ok`・`exhausted`・`unknown`）・`resets_at`（RFC 3339。無ければ `null`）。取れない枠は `available: false` で残り、ほかは `null` |
| `reason`（アカウントごと） | そのアカウントの値が取れない・名前が作れない理由。文字列の決まった値だけ：`without_usage`（`omp` の `accountsWithoutUsage` から来たアカウント。2 枠とも取得できず）・`no_email`（メールが空で、名前を `account-N` にしたアカウント）。どちらでもなければ `null`。両方に当てはまるときは `without_usage`。メール・`accountId`・`orgId`・`omp` の出力の文面は入れない（FR-REP-18）。読み手は知らない値を「取得できません」と扱う（値は将来足すことがある）。markdown には出さない。`host_status.usage` も同じ |

`omp` のキー：`status`（`up_to_date`・`update_available`・`unknown`・`pending`・`disabled`）、`current`・`latest`（`X.Y.Z` の形に合うものだけ。無ければ `null`）、`reason`（`unknown` のとき `not_found`・`timeout`・`exec`・`parse`、ほかは `null`）、`checked_at`（最後に確認した時刻。まだなら `null`）。

### 5.6 `dispatch`（`/ops` のみ）

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `pane` | string | 必須 | `shell` または `idle` のペイン |
| `session` | string | 任意 | [API 仕様 §4.1](endpoints.md#41-session-引数と許可セッション)。`pane` はこのセッションの中で解決する |
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

- 共通の条件 → プロファイルの条件 → `extra_rules` の順。`{report_path}` は最終報告ファイルの実パス（`/tmp/swarm-console/reports/<位置>.md`）に置換。`<位置>` はセッション内の位置ではなく全体の位置（既定セッションは `T1`、それ以外は `1:T1`）で、`:`・`/`・`\` は `_` にする（例 `1:T1` → `1_T1.md`。別のセッションのファイルが衝突しない。[er-config §7](../architecture/er-config.md#7-最終報告ファイル)）。
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
| `dispatch` | `/ops` | `session`・`pane`・`agent`・`cwd`・`title`・`command`・`task`・`profile`・`extra_rules`（`profile` と `task` は必須。`session` は引数の先頭で、許可セッションがちょうど 1 つなら省略でき、そうでなければ指定が要る。prompt の定義としては任意の引数。P-86） | 「この引数のまま `dispatch` ツールを呼び、返った `sent` と結果を報告する」というユーザーメッセージ（引数を描画する JSON でも `session` は先頭） |

プロンプトの文面はサーバー側で固定（クライアントによらず同じ）。

### 6.2 resources

| URI | MIME | 内容 |
|-----|------|------|
| `swarm://report` | `text/markdown` | `report`（markdown、既定言語）と同じ。許可セッションがちょうど 1 つのときだけ読める（そのセッションの report）。2 つ以上のトークンは `session_required` で、`resources/list` にも載せない |
| `swarm://pane/<位置>` | `text/markdown` | `report` の `pane` 指定（1 ペインの詳しい版） |
| `swarm://notes/<位置>` | `application/json` | `note_get` と同じ |

- `<位置>` は位置の名前または tmux ID。ID の `%` は URI では `%25`、位置の `:` は `%3A` にする（`swarm://pane/%2512`・`swarm://pane/1%3AT1`。`resources/list` はこの形で載せる）。許可セッションが 1 つのトークンでは、位置はそのセッション内の位置（接頭辞なし）か tmux ID。2 つ以上のトークンでは、全体の名前（既定セッション以外は `1:T1`、既定セッションは `T1`。全体の名前は一意）か tmux ID で、`resources/list` も全体の名前で載せる。
- `resources/list` は許可セッションのペインぶんの URI だけを列挙し、`resources/templates/list` にテンプレート（`swarm://pane/{pos}`・`swarm://notes/{pos}`）を返す。`resources/read` もツールと同じ許可の検査をする。
- エラーは JSON-RPC エラーに [API 仕様 §3.1](endpoints.md#31-形式) の本文を `data` として載せる：解決できない位置は `pane_not_found`、許可外のセッションのペインは `session_forbidden`（どちらも `-32002`）、`swarm://report` の `session_required` は `-32602`（invalid params）。

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
| `file_get` の 1 ファイル | 20 MiB（20971520 バイト。`file_get_max_bytes` で 1〜100 MiB に変更可） | `too_large` |
| リクエスト本文 | 256 KiB | 413 |
