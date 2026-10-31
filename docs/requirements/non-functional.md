# 非機能要件

元仕様 v1.1 の §3・§7・§8・§9 を非機能要件として整理する。数値目標のうち元仕様にないものは「提案」（`P-xx`、[アーキテクチャ設計 §9](../architecture/overview.md#9-提案した既定値と実装時確認事項)）で、実装後の計測で見直してよい。

## 1. 性能

| ID | 項目 | 目標 | 備考 |
|----|------|------|------|
| NFR-PERF-1 | `panes_status` の応答時間 | 8 ペインで p95 500 ms 以内（スナップショットが新しい場合は 50 ms 以内） | tmux 呼び出しはペイン並列。P-05 |
| NFR-PERF-2 | `report` の応答時間 | `panes_status` と同等 + 描画 20 ms 以内 | 同じスナップショットを使う |
| NFR-PERF-3 | 応答サイズ（文字量削減） | `panes_status` 8 ペインで 8 KB 以内、`report`（markdown）で 6 KB 以内、`ask_card` で 2 KB 以内 | 元仕様の目的 1。画面全文は返さない |
| NFR-PERF-4 | 操作ツールの応答時間 | 仕様で定めた待ち（`send_text` の 1 秒、`pane_close` の 1 秒×2×手順数など）＋ 受理確認の待ちで、`accept_timeout`（10 秒）以内に決着 | 超えたら `ok: false` と現在の `state` |
| NFR-PERF-5 | 常駐時の負荷 | ホスト負荷が高い状態（load1 ~100、32 CPU）でも劣化しない。サンプラーの CPU は 1 コア未満の数 % | 現在のホストの load1 は 98 |
| NFR-PERF-6 | メモリ | 常駐 100 MB 以内 | ペインのハッシュ・直近画面のみ保持 |
| NFR-PERF-7 | 取得量の上限 | `pane_tail` は最大 200 行・20,000 字。`report_read` は 20,000 字 | 上限を超えたら `truncated: true` |

高負荷ホストで動かすため、tmux コマンドにはそれぞれ 3 秒のタイムアウトを付け、ペインごとの呼び出しを並列にしすぎない（同時 8 まで）。

## 2. セキュリティ

| ID | 要件 |
|----|------|
| NFR-SEC-1 | 待ち受けは ousiass-desktop の tailnet IP のポート 48120 だけ。`0.0.0.0`・`::`・ワイルドカード・設定未指定は起動拒否。tailnet の範囲（`100.64.0.0/10` または `fd7a:115c:a1e0::/48`）か、テスト用のループバックのみ許可（P-01） |
| NFR-SEC-2 | インターネットに出さない（Funnel・Tunnel・silent 経由は使わない）。経路は box のユーザースペース tailscale から直接 |
| NFR-SEC-3 | 認証は Bearer トークン。クライアントごとに別のトークンを発行し、名前と窓口（`report`/`ops`）を付ける。なし・不一致は 401、窓口違いは 403。比較は定数時間（全登録トークンを必ず走査し、早期終了しない） |
| NFR-SEC-4 | トークンはサーバーにハッシュ（SHA-256）だけ保存する。`~/.config/swarm-console/tokens.toml` は権限 600。権限が 600 でなければ起動拒否。発行時に 1 度だけ平文を表示する（P-04） |
| NFR-SEC-5 | トークンの平文・ハッシュ・`Authorization` ヘッダーをログ・応答・エラー・メモに出さない。box 側は環境変数 `SWARM_CONSOLE_TOKEN` から読む |
| NFR-SEC-6 | トークンを 1 つだけ無効にできる（`token revoke <名前>`）。再起動なしで次のリクエストから効く（P-04） |
| NFR-SEC-7 | 任意のシェルコマンドは実行しない。許可するのは決まった tmux サブコマンド（`capture-pane`・`send-keys`・`load-buffer`・`paste-buffer`・`select-pane -T`・`display -p`・`list-panes`）と、決まったファイル・`/proc` の読み取りだけ。引数は `exec` の引数配列で渡し、シェルを経由しない |
| NFR-SEC-8 | `pane_start` の `cwd` は `/mnt/nvme/` の下と `/home/ousiass/dle/` の下だけ許可。悠太さんの checkout（設定の一覧）と `~/dotfiles` は拒否。シンボリックリンクは解決後のパスで判定する |
| NFR-SEC-9 | 送れる文字は通常の文字と Enter だけ。ESC を含む制御文字（bracketed paste の終端を偽装できるもの）を拒否する。Esc は `send_escape`（`confirm: true` 必須・`/ops` のみ） |
| NFR-SEC-10 | `/report` に操作のツールを出さない（`tools/list` にも載らない）。`/report` からは書き込み（メモを含む）ができない |
| NFR-SEC-11 | メモに秘密に見える文字列（`ghp_`・`sk-`・`eyJ`・PEM など）が入る書き込みは拒否し、拒否した値をログ・応答に出さない |
| NFR-SEC-12 | ログには「誰が（トークン名）・いつ・どのツールを・どのペインに使ったか・結果コード」だけを記録する。送った本文・トークン・メモの内容は記録しない |
| NFR-SEC-13 | 受け付けるリクエスト本文は 256 KiB まで。超えたら 413 |
| NFR-SEC-14 | 設定・メモ・トークンの置き場以外へ書き込まない（書き込み先は `~/.config/swarm-console`・`~/.local/share/swarm-console`・`/tmp/swarm-console`）。`~/dotfiles` には触らない |

## 3. 可用性・信頼性

| ID | 要件 |
|----|------|
| NFR-AVL-1 | systemd のユーザーサービス `swarm-console.service` で常駐し、落ちたら自動で立ち上げ直す（`Restart=always`、`RestartSec=2`）。ログイン外でも動くよう `loginctl enable-linger` が要る |
| NFR-AVL-2 | 状態を持つのはメモ（`notes.json`）とトークンだけ。画面ハッシュ・スナップショットは再起動で失ってよい（再起動直後は `changed_ago` が「起動以降」になる） |
| NFR-AVL-3 | メモの書き込みは一時ファイル → fsync → rename で行い、途中で落ちても壊れない。起動時に壊れたファイルを見つけたら `.corrupt-<時刻>` に退避し、空で始めて警告をログに残す |
| NFR-AVL-4 | tmux が落ちている・セッションがないときも落ちない。`panes` を空にして `host_status.tmux_session: false` と警告を返す |
| NFR-AVL-5 | 1 ペインの tmux 呼び出しの失敗・タイムアウトが他のペインの結果を止めない（そのペインを `unknown` にして警告） |
| NFR-AVL-6 | 同じペインへの操作は直列化し、デッドロックしない（`lock_timeout` で `busy_timeout`）。異なるペインは並行に処理できる |
| NFR-AVL-7 | 終了シグナル（SIGTERM）で進行中の操作を完了待ち（最大 10 秒）してから止まる。操作の途中で止まって、ペインに半端な入力を残さない |
| NFR-AVL-8 | 配布は GitHub Releases のバイナリ。サーバーでは手で入れ替えて `systemctl --user restart swarm-console` |

## 4. スケーラビリティ・容量

| ID | 要件 |
|----|------|
| NFR-SCL-1 | ペインの数・並びは決め打ちしない。対象セッション複数・合計 32 ペインまで目標性能を満たす（P-17） |
| NFR-SCL-2 | 同時接続クライアントは 10 まで（トークン数の目安）。同じペインへの操作は直列化される |
| NFR-SCL-3 | メモの履歴は 500 件、ペインごとの `log` は 50 行、`items` は 50 件、文字列は項目ごとの上限（ER §2.4）まで |
| NFR-SCL-4 | `notes.json` は最大でも約 2 MB を想定（500 件 × 数 KB）。全体を読み書きしてよい |

## 5. 運用性・保守性

| ID | 要件 |
|----|------|
| NFR-OPS-1 | 置き場所：バイナリ `~/bin/swarm-console/`、設定 `~/.config/swarm-console/`、データ `~/.local/share/swarm-console/`。`~/dotfiles` には触らない |
| NFR-OPS-2 | CLI：`swarm-console serve`（常駐）、`swarm-console token add <名前> --scope ops` または `--scope report`、`swarm-console token revoke <名前>`、`swarm-console version` |
| NFR-OPS-3 | エージェントの画面が変わったら、設定ファイルの正規表現の上書きだけで直せる（再ビルド不要）。設定は `serve` の再起動で反映 |
| NFR-OPS-4 | レポートのテンプレート（`templates/report.ja.md`・`report.en.md`）と条件のひな形（`profiles.toml`）は設定フォルダに置け、再ビルド不要。テンプレートの構文エラーは起動時に検出して拒否する |
| NFR-OPS-5 | ログは構造化（JSON Lines）で標準エラーへ出し、systemd の journal で見る |
| NFR-OPS-6 | 新しいエージェントを足すには、アダプタを 1 つ実装して登録するだけで済む構造にする |
| NFR-OPS-7 | 認証の期限情報（`agent_auth_status`）はファイルの場所を設定で変えられる。取れなければ「不明」を返し、エラーにしない |

## 6. 互換性・前提環境

| ID | 要件 |
|----|------|
| NFR-ENV-1 | OS は Linux（ousiass-desktop）。`/proc` が読めること |
| NFR-ENV-2 | tmux 3.x。`load-buffer -`（標準入力）と `paste-buffer -p`（bracketed paste）が使えること。起動時に `tmux -V` を読み、足りなければ警告（P-24） |
| NFR-ENV-3 | Go 1 バイナリ。MCP は公式 Go SDK（`github.com/modelcontextprotocol/go-sdk`）。プロトコルは Streamable HTTP（JSON-RPC: `initialize`・`tools/list`・`tools/call` ほか prompts・resources） |
| NFR-ENV-4 | リクエストヘッダーは `Content-Type: application/json`、`Accept: application/json, text/event-stream` |
| NFR-ENV-5 | クライアントを決め打ちしない。標準的な MCP クライアントが同じ見た目・同じ指示になること（見た目と指示文はサーバー側で組み立てる） |

## 7. テスト・CI 要件

| ID | 要件 |
|----|------|
| NFR-TEST-1 | 本物の tmux を一時セッション（専用ソケット `-L`）で立てて、各ツールを通す |
| NFR-TEST-2 | omp・Claude Code・Codex の画面は、実際の画面を写したサンプル出力を、テスト用の小さなプログラム（擬似エージェント）で再現する。本物のエージェントは起動しない |
| NFR-TEST-3 | 拒否が効くことを確かめる：認証なし・違うトークン・窓口違い（403）・許可していないフォルダ・作業中のペインを閉じる・Esc の `confirm` なし・制御文字・秘密文字列 |
| NFR-TEST-4 | メモの書き込みが途中で落ちても壊れないことを確かめる（書き込み途中の停止を注入して、読み直せること） |
| NFR-TEST-5 | 同一スナップショットに対して `report`（`ja`・`en`）と `dispatch` の組み立てが決定的であることを確かめる（ゴールデンファイル） |
| NFR-TEST-6 | 各アダプタについて、状態判定・質問カード読み取り・選択キーの列をサンプル画面の表で確かめる |
| NFR-TEST-7 | 非同期の待ちは固定時間の `sleep` に頼らず、準備完了の通知・上限付きの条件待ち（ポーリング）を使う。不安定なテストはタイムアウトを延ばさずコード側（待ち方・準備完了の通知）を直す |
| NFR-TEST-8 | CI は `go build`・`go vet`・`golangci-lint`・`go test -race`。tmux が要るテストは CI でも tmux を入れて動かす |
| NFR-TEST-9 | バグ修正には回帰テストを付ける |
| NFR-TEST-10 | 受け入れ確認（機能要件 §8）は、自動テストで担保できる範囲を CI で、実クライアント接続（Claude Code・Codex・Cursor・box の curl）が要る範囲を実機で行い、未確認の部分は報告に明記する |
| NFR-TEST-11 | タグ `v*` の push で、GitHub Releases に linux/amd64 のバイナリを載せるワークフローを用意する（P-20） |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-31 | 新規作成 | 初版 |
