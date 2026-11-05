# レポートに足す値の集め方（Issue #21〜#25・#86・#87、P-60〜P-69・P-93・P-94）

[機能要件：レポート](../requirements/functional-report.md)（FR-REP-10〜18）の「値をどう取るか」を定める。出す形（キー・単位・取れないときの値）は [API 仕様：レポート](../api/endpoints-report.md#55-report) と [API 仕様：サーバーの状態](../api/endpoints-state.md#53-サーバーの状態)。

前のレポート（`~/bin/kana-report.sh`・`kana-usage-fmt.py`・`kana-table.py`）は値の取り方の参考にした。バーなどの見た目は仕様に入れない（値だけを返す）。

## 1. 原則

| 原則 | 内容 |
|------|------|
| 値はスナップショットに入れる | 描画（markdown・JSON）は、スナップショットに入った値だけを使う。描画のなかでコマンド実行・ファイル読み取り・時計の参照をしない（FR-REP-7・10） |
| 遅い取得は別の収集器 | `omp usage --json`・`omp update --check` は数秒〜数十秒かかるので、`serve` が決まった間隔で別に実行し、**最後の結果を不変の値として保持**する。スナップショットは作る時点の最後の結果を持つ（読み取りの呼び出しで実行しない） |
| 取れない値は消さない | キーを残し、`available: false` と `null` を返す（FR-REP-11）。取得の失敗は固定の `reason` コードだけを持つ（コマンドの出力・エラー文・パスは出さない） |
| 秘密を出さない | メール・`accountId`・`orgId`・トークンは出さない。アカウントの名前は §2.2 の規則だけ |

## 2. 使用状況（Issue #21）

### 2.1 取得

| 項目 | 内容 |
|------|------|
| コマンド | `omp usage --json`（引数は固定。シェルを経由しない。標準入力は `/dev/null`。tty は使わない。この機で tty なしでも JSON が返ることを確認した） |
| 実行ファイル | `[agents.omp] command` の先頭の語（既定 `omp`）。絶対パスでなければ `~/.local/bin` を足した PATH で探す。見つからなければ `reason: not_found` |
| 間隔 | `usage_interval`（既定 `5m`、下限 `1m`）。`serve` 起動の 10 秒後に 1 回目。前の実行が終わるまで次を始めない |
| 打ち切り | 60 秒（超えたら `reason: timeout`）。標準出力は 2 MiB まで |
| 設定 | `usage_check`（既定 `true`）。`false` なら実行せず `status: disabled` |
| 失敗の扱い | 失敗しても `serve` は動き続ける。前の成功した結果があれば残し、`status: stale` にする。成功が一度も無ければ `unavailable` |

出力の読み方（`reports[]` の 1 要素 ＝ 1 アカウント）：

| 出力側 | 読むキー | 意味 |
|--------|----------|------|
| `provider` | `anthropic`・`openai-codex`・`cursor` | `claude`・`codex`・`cursor` に対応。ほかは捨てる |
| `metadata.email` | `@` より後ろ | アカウント名の元（§2.2）。値そのものは出さない |
| `limits[].scope`・`window.id` | Claude・Codex は `scope.shared` が `true` の `5h`・`7d` | `five_hour`・`seven_day`。`scope.tier`（`fable`）の枠は出さない（前のスクリプトと同じ） |
| `limits[].label` | Cursor は `Cursor Models`（ほかの枠は出さない） | `models` |
| `limits[].window.id` | Cursor は `monthly`・`weekly` など | `window` にそのまま入れる（アカウントで違う。この機は 1 つが `monthly`・`percent`、もう 1 つが `weekly`・`usd`） |
| `amount.used`・`amount.unit` | 数値と `percent`・`usd` | `used`（小数 1 位に丸める）・`unit`。`unit` が上の 2 つ以外なら、その枠は取得できず |
| `window.resetsAt` | epoch ミリ秒 | `resets_at`（RFC 3339、`at` と同じオフセット）。無ければ `null` |
| `status` | `ok`・`exhausted` | そのまま `status` に入れる。ほかの文字列は `unknown` |
| `generatedAt` | epoch ミリ秒 | トップの `fetched_at` |
| `accountsWithoutUsage` | 使用量が取れなかったアカウント | Claude なら、2 枠とも取得できずで出す（P-61）。アカウントごとの `reason` は `without_usage`（§2.2。P-94）。`accountsWithoutUsage[]` の要素に理由を表すキーはなく（実測。P-96）、ログイン切れ・権限・API エラーは区別できない |

### 2.2 アカウント名

メールの `@` 以降の最初のラベルを小文字にしたもの（例 `user@acme.example` → `acme`。前のスクリプトの `name()` と同じ）。ラベルが取れなければ `account-N`（N は、同じプロバイダのなかで並べたときの順番、1 始まり）。同じプロバイダで名前が重なったら、メール全体の昇順で 2 つ目から `-2`・`-3` を付ける。**メールのどの部分もそれ以外に出さない。**

アカウントごとの `reason`（`usage` の各アカウントのキー。P-94）は、名前の作り方を変えずに、次の決まった値だけを付ける。出力の文面・メール・`accountId`・`orgId` は入れない（FR-REP-18）。

| `reason` | 条件 |
|----------|------|
| `without_usage` | `accountsWithoutUsage` から来たアカウント（`reports` に同じプロバイダ・メールのものがあれば足さず、`reports` 側が勝つので `reason` は付かない。§2.1） |
| `no_email` | `accountsWithoutUsage` から来たのではなく、`metadata.email` が空で、名前を `account-N` にしたアカウント |
| なし（JSON は `null`） | それ以外 |

両方に当てはまるとき（`accountsWithoutUsage` から来て、メールも空）は `without_usage`（使用量が取れないことのほうが、見る人にとって大事なため）。メールがあるのに `@` 以降のラベルが使えず `account-N` になったアカウントは、メールが空ではないので `no_email` にしない（`null`）。Cursor も同じ規則で付ける。markdown には出さない。ログイン切れかどうかを別に調べない（`agent_auth_status` とも結び付けない）。

実機の `omp usage --json` では、`accountsWithoutUsage[]` の各要素に理由を表すキーがなく、`disabledCredentials`（トップレベルの別のキー）もアカウントと結びつかない。そのためログイン切れ・権限・API エラーの理由は区別できず、`reason` はこの 2 つだけのままにする（P-96）。

### 2.3 並びと欠け

- アカウントは `account` の昇順（辞書順）。前のスクリプトは使用率の降順だったが、スナップショットごとに並びが入れ替わらないよう名前順にした（P-62）。
- Claude は、`omp` が返したアカウントを**全部**出す。現在 5 つ（この機での確認）。ある枠が返ってこないアカウント（この機では 5 時間枠が返らないアカウントがある）は、その枠を `available: false` にして**アカウントを残す**。
- 一度でも取れたアカウントは、あとの取得が成功して一覧から消えるまで消さない（失敗の間は `stale`）。

## 3. サーバー負荷（Issue #23）

| 値 | 取り方 | 取れないとき |
|----|--------|-------------|
| `mem.total_gb`・`used_gb`・`used_percent` | `/proc/meminfo` の `MemTotal`・`MemAvailable`（KiB）。`used` ＝ `MemTotal − MemAvailable`（`free` コマンドの `used` に近く、`used_gb + mem_free_gb ≒ total_gb`）。GB は 1024³ で割って小数 1 位、`used_percent` は `100 × used ÷ total` を四捨五入した整数（スワップと同じ丸め）。`host.mem_free_gb` は `MemAvailable` のまま残す（`MemFree` ではない。P-93） | `/proc/meminfo` を読めない・項目がない・数値が不正（`host` 全体は捨てず、負荷・稼働時間・CPU・スワップ・ディスクは読めた値のまま。`host.mem_free_gb`・`host_status` の `mem_free_gb`・`mem_total_gb`・`panes_status` の `host.mem_free_gb` は 0 になるので、読み手は `report`・`host_status` の `mem.available` を見る（`panes_status` には取得できずの印がない）。P-95）・`MemTotal` が 0 以下・`MemAvailable` が `MemTotal` より大きい：`available: false`・数値は `null`。メモリのエラーは `Reader` が記録せず `host.Status.MemErr` で返し、スナップショットを作る側（`internal/pane/build.go`）が警告ログ `メモリを読めませんでした` に残す |
| `cpu.used_percent` | `/proc/stat` の先頭 `cpu` 行。**前のサンプラーの値との差**で `100 × (1 − (idle + iowait) / 合計)`、整数に丸める | 起動後の最初のサンプル（差が無い）・読めない・合計が増えていない：`available: false` |
| `swap.total_gb`・`used_gb`・`used_percent` | `/proc/meminfo` の `SwapTotal`・`SwapFree`（KiB）。GB は 1024³ で割って小数 1 位、`used_percent` は整数 | 読めない：`available: false`。`SwapTotal` が 0 なら `total_gb: 0`・`used_gb: 0`・`used_percent: 0`（スワップ無し。取得できずではない） |
| `disk[].total_gb`・`used_percent` | `disk_mounts` の各マウントを `statfs`。`used_percent` は `df` の `Use%` と同じ（使用 ÷ （使用 ＋ 一般ユーザーが使える空き）、切り上げの整数）。`free_gb` は今のとおり | `statfs` が失敗：そのマウントを残し `available: false` |

- 「SSD 2 台」は `disk_mounts`（既定 `/` と `/mnt/nvme`）の 2 つ。マウントを新しく見つけたり、デバイス名を設定に書いたりしない。台数は `disk_mounts` の要素数に従う（P-63）。
- CPU のサンプルは `poll_interval` ごとにサンプラーだけが更新する。読み取りの呼び出しで取り直したスナップショットは、直近のサンプラーの値を使う（短い間隔の差で値がぶれない。FR-REP-7）。
- 前のスクリプトのバーは I/O の忙しさ（`iostat` の `%util`）だったが、今回は Issue の言葉どおり「使用量」（容量の使用率）にした（P-63）。I/O の忙しさ・iowait・OOM は今回足さない。

## 4. omp の更新確認（Issue #24）

| 項目 | 内容 |
|------|------|
| コマンド | `omp update --check`（引数固定。実行ファイルは §2.1 と同じ。打ち切り 30 秒） |
| 間隔・設定 | swarm-console の確認と同じ `update_check`・`update_check_interval`（既定 24 時間、下限 1 時間）。`update_check = false` なら実行せず `status: disabled`。`serve` 起動の 90 秒後に 1 回目（FR-UPD-7 の 1 分後の確認と重ねない） |
| 読み方 | ANSI 制御文字を取り除いた出力から、`Current version: X.Y.Z` → `current`、`New version available: X.Y.Z` → `latest`、`Already up to date` を含む → 最新。この機で、最新のとき（`18.14.2`・`Already up to date`）と、更新ありのとき（`Current version: 18.14.2` ＋ `New version available: 18.15.0`）の両方の実出力を確認した（2026-11-02。P-64） |
| 結果 | `up_to_date`（`Already up to date`）・`update_available`（`New version available` があり、`latest` が `current` と違う）・`unknown`（上のどちらでもない・実行できない。`reason`：`not_found`・`timeout`・`exec`・`parse`）・`pending`（起動後まだ確認していない）・`disabled` |
| 版の形 | `X.Y.Z`（数字だけ。`^(0\|[1-9][0-9]*)\.(0\|[1-9][0-9]*)\.(0\|[1-9][0-9]*)$`）に合うものだけ出す。合わなければその値は `null`（出力の文面をそのまま返さない。FR-UPD-5 と同じ考え方） |
| 入れ替えない | `omp update` は実行しない（確認だけ。`--check` 以外の引数を渡さない） |

swarm-console 自身の更新状態は、`host_status.update` と同じ値を `report` の JSON（`update`）と markdown の見出し（「更新」の行）にも載せる（P-65。FR-UPD-9）。スナップショットを作るときに `serve` の確認の最後の結果を 1 回読んで入れる。追加の通信・設定は無い。

## 5. ログイン期限（Issue #25）

- 値は `agent_auth_status` と同じ判定（[agent-auth.md](agent-auth.md)。48 時間以内で `expiring_soon`、過ぎていれば `expired`、不明は `unknown`）を、スナップショットを作るときに 1 回読んで入れる。新しい閾値・設定は足さない（P-66）。
- 要対応の一覧には `expired` と `expiring_soon` だけ入れる。`unknown`（`auth_file` 未設定の omp など）は要対応に**入れない**。値としては残し、markdown の見出しに「不明」と出す（FR-REP-16）。
- 「あと何時間」は、`expires_at` と `Snapshot.At` から描画で計算する（呼び出し時刻を使わない）。

## 6. 起動直後・無効のとき

| 値 | 起動直後（最初の取得まで） | 設定で止めたとき |
|----|-------------------------|-----------------|
| `usage` | `status: pending`・アカウントは空 | `usage_check = false`：`disabled` |
| `omp` | `pending` | `update_check = false`：`disabled` |
| `cpu` | `available: false`（次のサンプラーまで） | — |

## 7. テスト

| 対象 | 方法 |
|------|------|
| 使用状況・omp の読み取り | 録った `omp usage --json`・`omp update --check` の出力（秘密を除いたもの）を固定して読ませる。5 アカウント・5 時間枠が無いアカウント・`unit: usd`・`exhausted`・`accountsWithoutUsage`・壊れた JSON・打ち切り。アカウントの `reason`（`without_usage`・`no_email`・`null`・両方に当てはまるときの優先）と、出力にメールが入らないこと |
| 実行 | 偽の `omp`（テスト用の実行ファイル）を `command` に指す。呼び出しの引数が固定の 2 つだけであること |
| CPU・メモリ・スワップ・ディスク | `/proc` 相当の固定入力。2 回目のサンプルで値が出る・差が無いと `available: false`・`MemTotal` が 0 のとき `mem` が `available: false`・スワップ無し |
| 同一性 | 同じスナップショットを固定して `ja`・`en`・`json` を 2 回描画し、1 文字も違わない（AC-1。`serve` の収集器が途中で結果を変えても、同じスナップショットの出力は変わらない） |
