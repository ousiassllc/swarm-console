# アーキテクチャ設計：デプロイ構成・設定ファイル

[アーキテクチャ設計](overview.md) の続き。§7 デプロイ構成と §8 設定ファイルの全体像を定める。

改訂履歴は [アーキテクチャ設計（本書）](overview.md#改訂履歴) にまとめてある。

## 7. デプロイ構成

| 項目 | 内容 |
|------|------|
| 実行場所 | example-host（tmux サーバーと同じホスト、同じユーザー） |
| バイナリ | `~/bin/swarm-console/swarm-console`（GitHub Releases から。最初の導入は `make install`。Releases への掲載は main の CI 成功ごとに自動。`serve` が自分で入れ替える。`update_auto = false` のときは手で入れ替え）。同じフォルダに `swarm-console.new`・`.old`・`.update-pending`・`.update-failed` が一時的にできる（ER §8） |
| 設定 | `~/.config/swarm-console/`：`config.toml`・`profiles.toml`・`tokens.toml`（600）・`templates/`（任意） |
| データ | `~/.local/share/swarm-console/notes.json`（600） |
| 最終報告 | `/tmp/swarm-console/reports/<位置>.md`（エージェントが書く）、`archive/`（サーバーが移す） |
| 待ち受け | `config.toml` の `listen_addr`（example-host の tailnet IP:48120） |
| 常駐 | systemd ユーザーサービス `swarm-console.service` |
| クライアント | クライアント環境：`sc.sh <tool> '<json>'`（SOCKS5 localhost:<port> 経由の curl、既定 `/ops`）。パソコンの AI ツールは MCP 設定に URL とトークンを入れる |

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
- 更新手順（`update_auto = false` のとき）：新バイナリを置く → `systemctl --user restart swarm-console` → `host_status` で確認。
- 最初の導入：リポジトリの直下で `make install`（`scripts/install.sh`。FR-INS）。最新リリースを取り、`SHA256SUMS` で照合してから `~/bin/swarm-console/swarm-console` に置き、`deploy/swarm-console.service` を `~/.config/systemd/user/` に実ファイルでコピーして `daemon-reload`・`enable`（`config.toml` があれば `--now`）、リンガリングを有効にする。`config.toml` は作らず、無いときは起動せず設定の手順を表示する。何回流しても同じ結果で、動いているサービスは再起動しない（FR-INS-6）。
- 新しい版の確認は `serve` が 24 時間ごとに `api.github.com` を読んで行い（`update_check`。FR-UPD）、結果は `host_status` の `update` と `version --check` で見る。`update_auto`（既定 `true`）なら、新しい版を見つけたときにダウンロード → 検証 → `rename` → 正常終了し、上の `Restart=always`・`RestartSec=2` が新バイナリを起動する（`systemctl` は実行しない）。
- 自己更新は `swarm-console.service` の変更を要しない。新バイナリが起動に失敗し続けても、3 回目の起動で `.old` に戻して終了する（`RestartSec=2` なので約 4 秒後。systemd の既定の起動回数の上限（10 秒に 5 回）より前。FR-UPD-17・P-55）。

## 8. 設定ファイルの全体像

`~/.config/swarm-console/config.toml`（キーの型と検証は [ER §5](er-config.md#5-configtoml)）。

| キー | 既定 | 意味 |
|------|------|------|
| `listen_addr` | （必須） | tailnet IP:48120 |
| `tmux_socket` | なし | tmux の `-L` ソケット名（テスト用） |
| `sessions` | `["0"]` | 対象セッション。トークンの `allowed_sessions`（`tokens.toml`）はこの部分集合（FR-SESS-1） |
| `default_session` | `sessions` の先頭（1 つだけのとき） | 位置の接頭辞を省略するセッション（ツールの許可・`session` の既定には使わない。P-87） |
| `default_lang` | `"ja"` | `report` の既定言語 |
| `poll_interval` | `5s` | サンプラー間隔 |
| `snapshot_ttl` | `2s` | 読み取りツールが再取得する閾値 |
| `stalled_after` | `15m` | 画面変化なしで `stalled` とする時間 |
| `layout_warning_ttl` | `10m` | 並び変更の警告を出し続ける時間 |
| `accept_timeout` / `start_timeout` / `lock_timeout` | `10s` / `30s` / `60s` | 操作の確認待ち・起動待ち・同ペイン待ち |
| `title_prefix` | `"agent: "` | `pane_start` のタイトル接頭辞 |
| `allowed_roots` | `["/mnt/nvme/", "~/projects/"]` | `cwd` を許可する根。`dir_list`（`/ops` のみ）が一覧する範囲でもある |
| `denied_paths` | `["~/dotfiles"]`（書いたら入れ替え。P-29） | `cwd`・`file`・`file_get` と `dir_list` の `path` を拒否するパス |
| `file_roots` | `["/tmp/", + allowed_roots]` | `send_text` の `file` を許可する根（P-13） |
| `file_get_roots` | `[]`（なし） | `file_get` の許可リストに足す根。ペインの `cwd` の下と `report_dir` の下は書かなくても常に取れる（ただし `cwd` が `/` やホームそのものなら、その `cwd` は除く。ここに書いた根はそのまま許可。P-91） |
| `file_get_max_bytes` | `20971520`（20 MiB。1〜104857600） | `file_get` が返す 1 ファイルの上限（P-91） |
| `disk_mounts` | `["/", "/mnt/nvme"]` | `host_status` が見るマウント |
| `report_dir` | `/tmp/swarm-console/reports` | 最終報告の置き場 |
| `update_check` | `true` | `serve` が GitHub Releases で新しい版を確認する（FR-UPD） |
| `update_check_interval` | `24h`（1h 以上） | 確認の間隔 |
| `usage_check` | `true` | `serve` が `omp usage --json` で使用状況を取る（FR-REP-12）。`omp` の確認（FR-REP-15）は `update_check` に従う |
| `usage_interval` | `5m`（1m 以上） | 使用状況の取得の間隔 |
| `update_auto` | `true` | 新しい版を見つけたら `serve` が自分で入れ替える（FR-UPD-19。`update_check = false` なら無効） |
| `[pane_names]` | なし | 位置の固定名 |
| `[agents.<name>]` | 組み込み | アダプタの上書き（コマンド・正規表現・認証ファイル） |
