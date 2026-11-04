# API 仕様：サーバーの状態・メモのツール

[API 仕様](endpoints.md) の続き。§5 ツールのうち、§5.3 サーバーの状態（`sessions_list`・`host_status`・`agent_auth_status`）と §5.4 メモ（`note_get`・`note_set`・`note_log`・`note_history`）を定める。

改訂履歴は [API 仕様（本書）](endpoints.md#改訂履歴) にまとめてある。

## 5. ツール（続き）

### 5.3 サーバーの状態

#### `sessions_list`

`session` を選ぶための一覧。`config.toml` の `sessions` の順に返す。`/report`・`/ops` の両方で使える。

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `all` | bool | 任意 | 既定 `false`。`true` なら許可されていないセッションもスタブで含める |

```json
{"sessions": [
  {"id": "0", "allowed": true, "operable": true, "layout": {"rows": 2, "cols": [3, 3]}, "panes": ["T1", "T2", "T3", "B1", "B2", "B3"]},
  {"id": "1", "allowed": false, "operable": false}
]}
```

| キー | 意味 |
|------|------|
| `id` | tmux セッション名（`sessions` の値そのもの） |
| `allowed` | 呼び出し元の許可セッションか（[API 仕様 §4.1](endpoints.md#41-session-引数と許可セッション)） |
| `operable` | `allowed` かつトークンのスコープが `ops` かつ `/ops` 経由のときだけ `true`。`/report` 経由は（`ops` のトークンでも）`false` |
| `layout` | 許可セッションだけ。そのセッションのペインから計算（`panes_status` の `layout` と同じ形） |
| `panes` | 許可セッションだけ。そのセッション内の位置（接頭辞なし）を位置順に |

- 既定は許可セッションだけを返す。`all: true` の許可外はスタブ `{"id", "allowed": false, "operable": false}`（`layout`・`panes` は出さない）で、ペインの中身は漏らさない。
- 許可されていて `sessions` にあるが tmux に無いセッションは `"layout": {"rows": 0, "cols": []}`・`"panes": []`（警告は `panes_status` の `session_missing`）。
- `session` は取らない（セッションを選ぶための一覧）。

#### `host_status`

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `session` | string | 任意 | [§4.1](endpoints.md#41-session-引数と許可セッション)。ホスト全体の値は変えず、`agents` と `tmux_session` だけそのセッションに絞る。許可が 2 つ以上のトークンが省略すると `session_required` |

```json
{"load": [98.2, 95.1, 90.4], "cpus": 32, "mem_total_gb": 128, "mem_free_gb": 41.3,
 "mem": {"available": true, "total_gb": 128.0, "used_gb": 86.7, "used_percent": 68},
 "cpu": {"available": true, "used_percent": 42},
 "swap": {"available": true, "total_gb": 8.0, "used_gb": 0.4, "used_percent": 5},
 "disk": [{"mount": "/", "available": true, "free_gb": 120, "total_gb": 450, "used_percent": 73},
          {"mount": "/mnt/nvme", "available": true, "free_gb": 830, "total_gb": 3700, "used_percent": 78}],
 "tmux_session": true, "agents": {"omp": 6, "claude": 0, "codex": 0}, "uptime": "12d 4h",
 "usage": {"status": "ok", "reason": null, "fetched_at": "2026-10-31T22:09:30+09:00",
           "claude": [{"account": "acct-a", "reason": null, "five_hour": "...", "seven_day": "..."}, "... report と同じ"], "cursor": [], "codex": []},
 "omp": {"status": "up_to_date", "current": "18.14.2", "latest": "18.14.2", "reason": null, "checked_at": "2026-10-31T09:00:00+09:00"},
 "update": {"status": "up_to_date", "current": "v0.1.5", "latest": "v0.1.5", "checked_at": "2026-11-01T12:00:00+09:00",
            "apply": {"state": "idle"}}}
```

`disk` は設定した監視マウント（既定 `/` と `/mnt/nvme`。SSD 2 台はこの 2 つ）。`agents` は種別ごとのペイン数。`session`（または許可が 1 つのときの省略）で決まったセッションについて、`agents` はそのセッションのペインだけを数え、`tmux_session` はそのセッションにペインがあるかを返す。`load`・`cpus`・`mem_*`・`cpu`・`swap`・`disk`・`uptime`・`usage`・`omp`・`update` はセッションによらずホスト全体の値。

`mem`・`cpu`・`swap`・`disk`（`available`・`total_gb`・`used_percent`）・`usage`（アカウントごとの `reason` を含む）・`omp` は、`report` の JSON と同じ形・同じ値（`mem` は `host.mem` に当たる。`mem_total_gb`・`mem_free_gb` は互換のため残す。形と取れないときの値は [API 仕様 §5.5](endpoints-report.md#55-report)、取り方は [report-values.md](../architecture/report-values.md)）。FR-REP-17。値はスナップショットに入っていたもので、呼び出しでコマンドを実行しない。取れない値はキーを残し `available: false` と `null` にする（FR-REP-11）。

`update` は新しい版の確認の結果と、自己更新の状況 `apply`（FR-UPD-9・20）。`serve` が持つ最後の結果を返すだけで、呼び出しで GitHub へは通信しない。

| `status` | 意味 | 他に付くキー |
|----------|------|-------------|
| `up_to_date` | 最新版以上 | `current`・`latest`・`checked_at`・`apply` |
| `update_available` | 最新版が現在より新しい | `current`・`latest`・`checked_at`・`apply` |
| `unknown` | 確認できない | `current`・`reason`（`network`・`rate_limit`・`access`・`no_release`・`release_invalid`・`dev_build`）・`checked_at`（前回の成功した結果があれば `latest` も残す）・`apply` |
| `pending` | 起動後まだ確認していない | `current`・`apply` |
| `disabled` | `update_check = false` | `current`・`apply` |

`apply` は常に付く（自己更新の状況。FR-UPD-20）。

| `state` | 意味 | 他に付くキー |
|---------|------|-------------|
| `off` | `update_auto = false`、または `update_check = false`（自己更新しない） | なし |
| `idle` | 入れ替えの失敗・保留が無い（新しい版が無い・まだ確認していないときのほか、入れ替えの実行中を含む。入れ替えが済めば `serve` は終了するので、`update_available` で `idle` を見るのは入れ替えの実行中だけ） | なし |
| `probation` | 入れ替え直後の試運転中（待ち受けから 60 秒生きれば確定。この間は新しい入れ替えを始めない。試運転中に見送った入れ替えは、確定の時点で次の周期を待たずに試す。FR-UPD-12・17） | `from`（入れ替え前の版） |
| `failed` | 入れ替えに至らなかった。次の周期で再試行 | `reason`（`unsupported_platform`・`not_managed`・`asset_missing`・`download`・`checksum`・`write`）・`at` |
| `blocked` | 起動に失敗して前の版へ戻した版（以下）なので入れ替えない | `version`（戻した版） |

`current` は動いているバイナリの版（版を入れていないビルドは `"dev"`）。`latest`・`from`・`version` は `vX.Y.Z` の形に合うときだけ出す。GitHub の応答の文面・URL・エラー文は出さない。

#### `agent_auth_status`

引数なし（`session` は取らない。ホスト上のログイン期限で、ペインに属さない）。

```json
{"agents": [
  {"agent": "omp", "status": "unknown", "expires_at": null},
  {"agent": "claude", "status": "valid", "expires_at": "2026-11-20T09:00:00+09:00"},
  {"agent": "codex", "status": "expiring_soon", "expires_at": "2026-11-01T08:00:00+09:00"}
]}
```

`status` は `valid`（有効）・`expired`（期限切れ）・`expiring_soon`（48 時間以内に切れる）・`unknown`（不明）。鍵・トークンの中身は出さない（読み先は P-16）。

### 5.4 メモ

#### `note_get`

| 引数 | 型 | 必須 |
|------|-----|------|
| `pane` | string | 必須 |
| `session` | string | 任意 |

```json
{"pane": "B4", "id": "%12", "note": {"from": "agent-b", "origin": "agent", "started_by": "かな", "repo": "ousiassllc/funid", "items": ["#982"], "next": "完了したら #983 を入れる", "log": [], "updated_at": "2026-10-31T21:50:12+09:00"}}
```

メモがなければ `"note": null`。`pane` は対象のセッション内の位置（接頭辞なし）。

#### `note_set`（`/ops` のみ）

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `pane` | string | 必須 | |
| `session` | string | 任意 | [§4.1](endpoints.md#41-session-引数と許可セッション) |
| `set` | object | 必須 | 書き換える項目だけ。値が `null` ならその項目を消す。キーは ER §2.2 の項目（`updated_at`・`log` は不可） |

```json
{"ok": true, "note": {"...": "更新後のメモ全体"}}
```

#### `note_log`（`/ops` のみ）

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `pane` | string | 必須 | |
| `session` | string | 任意 | [§4.1](endpoints.md#41-session-引数と許可セッション) |
| `text` | string | 必須 | 1 行・500 字まで |

```json
{"ok": true, "log_size": 12}
```

#### `note_history`

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `repo` | string | 任意 | 完全一致 |
| `from` | string | 任意 | 完全一致 |
| `origin` | string | 任意 | 完全一致（`user`・`agent`。ほかの値を渡すと 0 件になるだけで、エラーにはしない） |
| `started_by` | string | 任意 | 完全一致 |
| `items` | string[] | 任意 | 指定したものをすべて含む |
| `limit` | int | 任意 | 既定 20、1〜100 |

```json
{"entries": [{"pane_id": "%4", "pos": "B1", "agent": "omp", "title": "agent: funid #981", "closed_at": "2026-10-31T18:40:00+09:00", "closed_by": "pane_close", "from": "agent-a", "origin": "agent", "started_by": "かな", "repo": "ousiassllc/funid", "items": ["#981"], "log": []}], "total": 1}
```

新しい順。`total` は絞り込み後の件数（`limit` 前）。指定した絞り込みは AND。履歴には `from` と `origin`・`started_by` が両方残る（`from` は依頼元、`origin`・`started_by` は誰が始めたか。ER §2.2）。`session` は取らない（`repo`・`from`・`origin`・`started_by`・`items` だけで絞り、ペインのセッションでは絞らない。既知の限界。P-89）。
