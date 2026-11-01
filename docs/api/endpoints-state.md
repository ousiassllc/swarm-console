# API 仕様：サーバーの状態・メモのツール

[API 仕様](endpoints.md) の続き。§5 ツールのうち、§5.3 サーバーの状態（`host_status`・`agent_auth_status`）と §5.4 メモ（`note_get`・`note_set`・`note_log`・`note_history`）を定める。

改訂履歴は [API 仕様（本書）](endpoints.md#改訂履歴) にまとめてある。

## 5. ツール（続き）

### 5.3 サーバーの状態

#### `host_status`

引数なし。

```json
{"load": [98.2, 95.1, 90.4], "cpus": 32, "mem_total_gb": 128, "mem_free_gb": 41.3,
 "cpu": {"available": true, "used_percent": 42},
 "swap": {"available": true, "total_gb": 8.0, "used_gb": 0.4, "used_percent": 5},
 "disk": [{"mount": "/", "available": true, "free_gb": 120, "total_gb": 450, "used_percent": 73},
          {"mount": "/mnt/nvme", "available": true, "free_gb": 830, "total_gb": 3700, "used_percent": 78}],
 "tmux_session": true, "agents": {"omp": 6, "claude": 0, "codex": 0}, "uptime": "12d 4h",
 "usage": {"status": "ok", "reason": null, "fetched_at": "2026-10-31T22:09:30+09:00", "claude": ["... report と同じ"], "cursor": [], "codex": []},
 "omp": {"status": "up_to_date", "current": "18.14.2", "latest": "18.14.2", "reason": null, "checked_at": "2026-10-31T09:00:00+09:00"},
 "update": {"status": "up_to_date", "current": "v0.1.5", "latest": "v0.1.5", "checked_at": "2026-11-01T12:00:00+09:00",
            "apply": {"state": "idle"}}}
```

`disk` は設定した監視マウント（既定 `/` と `/mnt/nvme`。SSD 2 台はこの 2 つ）。`agents` は種別ごとのペイン数。

`cpu`・`swap`・`disk`（`available`・`total_gb`・`used_percent`）・`usage`・`omp` は、`report` の JSON と同じ形・同じ値（形と取れないときの値は [API 仕様 §5.5](endpoints-report.md#55-report)、取り方は [report-values.md](../architecture/report-values.md)）。FR-REP-17。値はスナップショットに入っていたもので、呼び出しでコマンドを実行しない。取れない値はキーを残し `available: false` と `null` にする（FR-REP-11）。

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

引数なし。

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

```json
{"pane": "B4", "id": "%12", "note": {"from": "agent-b", "repo": "ousiassllc/funid", "items": ["#982"], "next": "完了したら #983 を入れる", "log": [], "updated_at": "2026-10-31T21:50:12+09:00"}}
```

メモがなければ `"note": null`。

#### `note_set`（`/ops` のみ）

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `pane` | string | 必須 | |
| `set` | object | 必須 | 書き換える項目だけ。値が `null` ならその項目を消す。キーは ER §2.2 の項目（`updated_at`・`log` は不可） |

```json
{"ok": true, "note": {"...": "更新後のメモ全体"}}
```

#### `note_log`（`/ops` のみ）

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `pane` | string | 必須 | |
| `text` | string | 必須 | 1 行・500 字まで |

```json
{"ok": true, "log_size": 12}
```

#### `note_history`

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `repo` | string | 任意 | 完全一致 |
| `from` | string | 任意 | 完全一致 |
| `items` | string[] | 任意 | 指定したものをすべて含む |
| `limit` | int | 任意 | 既定 20、1〜100 |

```json
{"entries": [{"pane_id": "%4", "pos": "B1", "agent": "omp", "title": "agent: funid #981", "closed_at": "2026-10-31T18:40:00+09:00", "closed_by": "pane_close", "from": "agent-a", "repo": "ousiassllc/funid", "items": ["#981"], "log": []}], "total": 1}
```

新しい順。`total` は絞り込み後の件数（`limit` 前）。
