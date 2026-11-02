# 非機能要件

元仕様 v1.1 の §3・§7・§8・§9 を非機能要件として整理する。数値目標のうち元仕様にないものは「提案」（`P-xx`、[アーキテクチャ設計 §9](../architecture/decisions.md#9-提案した既定値と実装時確認事項)）で、実装後の計測で見直してよい。

## 1. 性能

| ID | 項目 | 目標 | 備考 |
|----|------|------|------|
| NFR-PERF-1 | `panes_status` の応答時間 | 8 ペインで p95 500 ms 以内（スナップショットが新しい場合は 50 ms 以内） | tmux 呼び出しはペイン並列。P-05 |
| NFR-PERF-2 | `report` の応答時間 | `panes_status` と同等 + 描画 20 ms 以内 | 同じスナップショットを使う |
| NFR-PERF-3 | 応答サイズ（文字量削減） | **よくある入力での目標**。単位は **バイト**（UTF-8。日本語 1 字は 3 バイト。KB は 1,024 バイト）。`panes_status` 8 ペインで 8 KB 以内、`report`（markdown）で 6 KB 以内、`ask_card` で 2 KB 以内。前提：`panes_status` と `report` は 8 ペインで、メモの `next` は数十字・`items` は数件・`activity` は 1 行（120 字以内）。`ask_card` は `question` が 1〜2 文・選択肢が数個（`label` は数十字）。API 仕様 §7 の上限いっぱいの入力（メモの文字列 2,000 字、選択肢 20 個×300 字など）までは保証しない | 元仕様の目的 1。画面全文は返さない。テスト（`rendertest/size_test.go`・`tooltests/size_test.go`）は上の前提の入力を表す |
| NFR-PERF-4 | 操作ツールの応答時間 | 仕様で定めた待ち（`send_text` の 1 秒、`pane_close` の 1 秒×2×手順数など）＋ 受理確認の待ちで、`accept_timeout`（10 秒）以内に決着。ただし `pane_close` は待ちが 2 つあるので、上限は 仕様で定めた待ち ＋ `shell` に戻るまでの待ち（`accept_timeout`）＋ 画面確認の待ち（`accept_timeout`） | 超えたら `ok: false` と現在の `state`。例外：`pane_close` で最初の待ち（終了コマンドのあと `shell` に戻るまで）を超えたときは `ok: false` ではなく `exit_timeout` のエラーを返す（機能要件 FR-O5・FR-O-C2） |
| NFR-PERF-5 | 常駐時の負荷 | ホスト負荷が高い状態（load average が CPU 数を大きく上回る状態）でも劣化しない。サンプラーの CPU は 1 コア未満の数 % | 負荷が高い開発機で確認する |
| NFR-PERF-6 | メモリ | 常駐 100 MB 以内 | ペインのハッシュ・直近画面のみ保持 |
| NFR-PERF-7 | 取得量の上限 | `pane_tail` は最大 200 行・20,000 字。`report_read` は 20,000 字 | 上限を超えたら `truncated: true` |

高負荷ホストで動かすため、tmux コマンドにはそれぞれ 3 秒のタイムアウトを付け、ペインごとの呼び出しを並列にしすぎない（同時 8 まで）。

## 2. セキュリティ

| ID | 要件 |
|----|------|
| NFR-SEC-1 | 待ち受けは example-host の tailnet IP のポート 48120 だけ。`0.0.0.0`・`::`・ワイルドカード・設定未指定は起動拒否。tailnet の範囲（`100.64.0.0/10` または `fd7a:115c:a1e0::/48`）の IP は **ポート 48120 以外も起動拒否**。テスト用のループバックだけはどのポートでも許可（P-01・P-73） |
| NFR-SEC-2 | インターネットに出さない（Funnel・Tunnel・silent 経由は使わない）。経路は クライアントのユーザースペース tailscale から直接 |
| NFR-SEC-3 | 認証は Bearer トークン。クライアントごとに別のトークンを発行し、名前と窓口（`report`/`ops`）を付ける。なし・不一致は 401、窓口違いは 403。比較は定数時間（全登録トークンを必ず走査し、早期終了しない） |
| NFR-SEC-4 | トークンはサーバーにハッシュ（SHA-256）だけ保存する。`~/.config/swarm-console/tokens.toml` は権限 600。権限が 600 でなければ起動拒否。発行時に 1 度だけ平文を表示する（P-04） |
| NFR-SEC-5 | トークンの平文・ハッシュ・`Authorization` ヘッダーをログ・応答・エラー・メモに出さない。クライアント側は環境変数 `SWARM_CONSOLE_TOKEN` から読む |
| NFR-SEC-6 | トークンを 1 つだけ無効にできる（`token revoke <名前>`）。再起動なしで次のリクエストから効く（P-04） |
| NFR-SEC-7 | 任意のシェルコマンドは実行しない。許可するのは決まった tmux サブコマンド（`capture-pane`・`send-keys`・`load-buffer`・`paste-buffer`・`delete-buffer`（`Paste` が自分で作った一意名のバッファを、貼り付けの失敗時・中断時に消すときだけ。`-b <その名前>` を必ず付け、ほかのバッファは消さない）・`select-pane -T`・`display -p`・`list-panes`）と、決まったファイル・`/proc` の読み取りだけ。例外は、レポートの値のための **引数固定の 2 コマンド** `omp usage --json` と `omp update --check` だけ（FR-REP-12・15。`[agents.omp] command` の先頭の語の実行ファイルを、ほかの引数なしで、標準入力 `/dev/null`・打ち切り付きで実行する。`omp update` など、ほかの `omp` のサブコマンド・引数は実行しない）。引数は `exec` の引数配列で渡し、シェルを経由しない。`omp` のコマンドが行う通信は `omp` の仕様で、`swarm-console` 自身は通信しない（NFR-SEC-15 は変えない） |
| NFR-SEC-8 | `pane_start` の `cwd` は `/mnt/nvme/` の下と `~/projects/` の下だけ許可。`denied_paths` に書いたパスは拒否（既定は `~/dotfiles` だけ。オーナーの checkout も拒否したいなら `denied_paths` に書く。P-29）。シンボリックリンクは解決後のパスで判定する |
| NFR-SEC-9 | 送れる文字は通常の文字と Enter だけ。ESC を含む制御文字（bracketed paste の終端を偽装できるもの）を拒否する。Esc は `send_escape`（`confirm: true` 必須・`/ops` のみ） |
| NFR-SEC-10 | `/report` に操作のツールを出さない（`tools/list` にも載らない）。`/report` からは書き込み（メモを含む）ができない |
| NFR-SEC-11 | メモに秘密に見える文字列（`ghp_`・`sk-`・`eyJ`・PEM など）が入る書き込みは拒否し、拒否した値をログ・応答に出さない |
| NFR-SEC-12 | ログには「誰が（トークン名）・いつ・どのツールを・どのペインに使ったか・結果コード」だけを記録する。送った本文・トークン・メモの内容は記録しない |
| NFR-SEC-13 | 受け付けるリクエスト本文は 256 KiB まで。超えたら 413 |
| NFR-SEC-14 | 設定・メモ・トークン・バイナリの置き場以外へ書き込まない。書き込み先は `~/.config/swarm-console`・`~/.local/share/swarm-console`・`/tmp/swarm-console` と、自己更新のための `~/bin/swarm-console/` の決まった 5 ファイルだけ（`swarm-console`・`swarm-console.new`・`swarm-console.old`・`swarm-console.update-pending`・`swarm-console.update-failed`。NFR-SEC-16）。`~/dotfiles` には触らない |
| NFR-SEC-15 | 新しい版の確認と自己更新（FR-UPD）のための外向き通信は、確認の `https://api.github.com/repos/ousiassllc/swarm-console/releases/latest` への認証なしの GET 1 本と、更新のダウンロード（`https://github.com/ousiassllc/swarm-console/releases/download/<tag>/…` の 2 ファイルと、そのリダイレクト先の `objects.githubusercontent.com`・`release-assets.githubusercontent.com`）だけ。どちらも認証なしで、トークン・設定の値・ペインの内容を送らない（送るのは `User-Agent: swarm-console/<版>` まで）。リダイレクトは許可リスト内の `https` だけ。待ち受け（NFR-SEC-1・2）の方針は変えない。GitHub が返した文字列は、`vX.Y.Z` の形に合うときだけ応答・表示・URL の組み立てに使う |
| NFR-SEC-16 | 自己更新は新しいバイナリを入れ替える前に実行せず、`systemctl` などのコマンドも実行しない（NFR-SEC-7 のまま）。再起動は自分で正常終了して systemd に任せる。入れ替えるのは、`SHA256SUMS` のハッシュが一致した、linux/amd64 の、現在より新しい版だけ。`os.Executable()` が `$HOME/bin/swarm-console/swarm-console` でない（シンボリックリンク・別の場所）なら入れ替えない。検証がハッシュだけである限界は機能要件 §12.5 に明記する |

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
| NFR-AVL-8 | 配布は GitHub Releases のバイナリ（main の CI が成功するたびに自動でリリースされる。NFR-TEST-11）。`serve` は新しい版を見つけたら自分で入れ替える（自己更新。`update_auto` が既定 `true`。FR-UPD-11）。`update_auto = false` のときは、手で入れ替えて `systemctl --user restart swarm-console` する |
| NFR-AVL-9 | 新しい版の確認・ダウンロード・入れ替えが失敗・遅延・拒否されても `serve` は動き続ける。確認と更新は起動・ツールの応答・終了（NFR-AVL-7）を待たせない（FR-UPD-8） |
| NFR-AVL-10 | 自己更新の途中で失敗・停止しても、元のバイナリが残る（入れ替えは一時ファイル → 検証 → `rename`。旧バイナリは `.old` に 1 世代残す）。新しいバイナリが起動に失敗し続けたら（3 回目の起動）、`.old` から自動で戻し、その版は再び入れ替えない（FR-UPD-15・17）。入れ替えの終了は NFR-AVL-7 と同じ正常終了で、`working` のペインがあっても待たない（ペインは tmux に残る。MCP クライアントは再接続が要る） |

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
| NFR-OPS-1 | 置き場所：バイナリ `~/bin/swarm-console/`（自己更新の対象はここに固定。FR-UPD-12）、設定 `~/.config/swarm-console/`、データ `~/.local/share/swarm-console/`。`~/dotfiles` には触らない |
| NFR-OPS-2 | CLI：`swarm-console serve`（常駐）、`swarm-console setup`（対話型の設定。FR-SET）、`swarm-console token add <名前> --scope ops` または `--scope report`、`swarm-console token revoke <名前>`、`swarm-console version`（`--check` で新しい版を確認。FR-UPD-6） |
| NFR-OPS-3 | エージェントの画面が変わったら、設定ファイルの正規表現の上書きだけで直せる（再ビルド不要）。設定は `serve` の再起動で反映 |
| NFR-OPS-4 | レポートのテンプレート（`templates/report.ja.md`・`report.en.md`）と条件のひな形（`profiles.toml`）は設定フォルダに置け、再ビルド不要。テンプレートの構文エラーは起動時に検出して拒否する |
| NFR-OPS-5 | ログは構造化（JSON Lines）で標準エラーへ出し、systemd の journal で見る |
| NFR-OPS-6 | 新しいエージェントを足すには、アダプタを 1 つ実装して登録するだけで済む構造にする |
| NFR-OPS-7 | 認証の期限情報（`agent_auth_status`）はファイルの場所を設定で変えられる。取れなければ「不明」を返し、エラーにしない |
| NFR-OPS-8 | `config.toml` は手書きと `setup` のどちらでも作れ、`setup` は手で書いた内容（コメント・並び・書式・`setup` が聞かないキー）を壊さない（FR-SET-3）。`setup` が書くのは設定の置き場（`~/.config/swarm-console`）の `config.toml` と、発行したときの `tokens.toml` だけ（NFR-SEC-14）。`config.toml` がシンボリックリンクのときは書かず中止する（リンク先が置き場の外のため。P-35） |
| NFR-OPS-9 | `setup` は `tmux`・`tailscale` などの外部コマンドを実行せず、ネットワークにも出ない。tailnet の候補はインターフェースの列挙だけで出す（NFR-SEC-7 と同じ方針。P-33） |
| NFR-OPS-10 | 新しい版の確認と自己更新は `config.toml` の `update_check`（既定 `true`）・`update_check_interval`（既定 `24h`、`1h` 以上）・`update_auto`（既定 `true`）で切り替える。`version --check` は設定に依らず手動で動く（確認だけで入れ替えない。FR-UPD-10・19） |
| NFR-OPS-11 | リリースの権限：`release.yml` の既定は `contents: read`。書き込み（`contents: write`）を持つのはリリースを作るジョブだけで、ビルドのジョブは書き込みトークンを持たない。PR（フォーク含む）のコードではリリースのワークフローを動かさない。リリースは同時に 1 本だけ（`concurrency`）。タグの版は `vX.Y.Z` だけで、Issue #7 の版の確認が読む形と同じ |
| NFR-OPS-12 | レポートの値の収集（`omp usage --json`・`omp update --check`）は `config.toml` の `usage_check`（既定 `true`）・`usage_interval`（既定 `5m`、`1m` 以上）と、`update_check`・`update_check_interval`（omp の確認。FR-REP-15）で切り替える。止めた値は `disabled`、起動直後は `pending` を返す |
| NFR-OPS-13 | `make install` は `$HOME` 配下（`~/bin/swarm-console`・`~/.config/systemd/user`）だけを書き、`sudo` を使わず、`config.toml`・`tokens.toml` を作らない。動いているサービスを再起動しない。ネットワークに出るのは GitHub（`github.com` と、リリースの実体の転送先）だけで、最新版の確認とバイナリ・`SHA256SUMS` の取得に使う（FR-INS） |

## 6. 互換性・前提環境

| ID | 要件 |
|----|------|
| NFR-ENV-1 | OS は Linux（example-host）。`/proc` が読めること |
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
| NFR-TEST-8 | CI は `go build`・`go vet`・`golangci-lint`・`linterly check`・release plan test（`plan-release_test.sh`。NFR-TEST-15）・install script test（`install_test.sh`。NFR-TEST-18）・`go test -race` をこの順に別ステップで動かす（骨子は [環境構築：CI/CD](../environment/setup-ci.md#cicd)）。tmux が要るテストは CI でも実 tmux で動かす。CI は GitHub ホストのランナー（`ubuntu-latest`）で動き、tmux はジョブの中で `apt-get install` して入れ、`tmux -V` で確かめる。Go・golangci-lint は `actions/setup-go`・golangci-lint の公式 action で入れる |
| NFR-TEST-9 | バグ修正には回帰テストを付ける |
| NFR-TEST-10 | 受け入れ確認（機能要件 §8）は、自動テストで担保できる範囲を CI で、実クライアント接続（Claude Code・Codex・Cursor・curl）が要る範囲を実機で行い、未確認の部分は報告に明記する |
| NFR-TEST-11 | 次の条件で、GitHub Releases に linux/amd64 のバイナリ（`swarm-console_vX.Y.Z_linux_amd64`）と `SHA256SUMS` を載せるワークフローを用意する（P-20・P-48。名前と書式は Issue #7 の自己更新が読む。環境構築 CI/CD）：main への push の CI が成功したら次の版のタグを自動で付けてリリースする（毎回パッチを上げる）。CI が失敗・PR・main の先頭でないコミットではタグを付けない。手でタグ `vX.Y.Z` を打てばマイナー・メジャーを上げられる |
| NFR-TEST-12 | `setup` の書き戻し（`config` の編集部品）は表駆動の単体テストで確かめる：コメント・空行・並び・CRLF・複数行配列・dotted key・表の有無・未知のキー（`serve` では拒否されるが編集部品は壊さない）。変更した値の範囲の外のバイト列が入力と同一であること、読み直した値が期待どおりであること、`serve` と同じ読み込みで落ちる結果は書かないことを確かめる |
| NFR-TEST-13 | `setup` の画面は、実 tmux の専用ソケットのペインで `swarm-console setup` を起動し、`send-keys` で入力して確かめる（NFR-TEST-1 と同じ方式。待ちは NFR-TEST-7）。新規作成 → そのまま `serve` が起動できる、手書きの `config.toml` を開いて 1 項目だけ変えても他が 1 バイトも変わらない、TTY でない起動が終了コード 1 で終わる、中止で何も書かれない、を通す |
| NFR-TEST-14 | 新しい版の確認は、実際の GitHub に出ずに `httptest` で確かめる：新しい版・同じ版・古い版（手元が先）・プレリリース形の現在の版・非 semver の最新タグ・`dev` ビルド・404・429・403（上限／拒否）・5xx・接続失敗・タイムアウト・不正 JSON・巨大な本文。`serve` の定期確認は `clock` を進めて確かめ（NFR-TEST-7）、確認が失敗しても `serve` が応答し続けること、`host_status.update` の各状態（`pending`・`disabled`・結果）を通す |
| NFR-TEST-15 | 次の版を決める処理（`.github/scripts/plan-release.sh`）は表駆動の `bash` テストで確かめる：タグなし → `v0.1.0`、最新 `v0.1.9` → `v0.1.10`（数として比較する）、手で打った `v1.0.0` の次は `v1.0.1`、プレリリース形・`v` 無し・`vX.Y` の拒否、main の先頭でないコミット、既に `v*` タグがあるコミット、手で打ったタグが既存の最大以下、を通す。ワークフロー全体は main に入れてからの最初の CI 成功で確かめる（PR 上では `workflow_run` が動かないため、結果は報告に明記する） |
| NFR-TEST-16 | 自己更新は、実際の GitHub にも実際の `~/bin` にも触れずに確かめる（`httptest` の偽の配信元、`HOME` を一時ディレクトリに差し替え。パスは `$HOME` から決める）：成功（入れ替え・`.old`・マーカー・終了の要求）、`SHA256SUMS` の不一致・行なし・行が複数・形式不正・空のバイナリ、404（`asset_missing`）、サイズ超過、許可されないホストへのリダイレクト、`os.Executable()` が置き場と違う・シンボリックリンク、ダウングレード・同じ版、`update_auto = false`・`update_check = false`、`blocked`（`update-failed` の版以下）、各ステップ（書き込み・リンク・マーカー・rename）の失敗で本体が元のまま残ること、`SIGTERM` での中断と一時ファイルの掃除。試運転は `boots` の 1・2・3 回目、60 秒（`clock`）での確定、`to` が自分の版でないマーカーの掃除、`.old` が無いときの戻し失敗を通す。`working` のペインがあっても待たずに入れ替えることを確かめる。実際の systemd による再起動と戻し（新バイナリの起動失敗）は CI で再現できないので、実機で確認し、未確認なら報告に明記する（NFR-TEST-10 と同じ扱い） |
| NFR-TEST-17 | レポートに足した値（FR-REP-10〜18）は、実際の `omp` にも実際の `/proc` にも依らずに確かめる：録った `omp usage --json`・`omp update --check` の出力（メール・`accountId` を除いた形）と偽の実行ファイルを使い、5 アカウント・5 時間枠が無いアカウント・`unit: usd`・`exhausted`・`accountsWithoutUsage`・壊れた JSON・打ち切り・`not_found`・`disabled`・`pending`・`stale`、CPU の 1 回目（取得できず）と 2 回目、スワップ無し、`statfs` 失敗、`expired`・`expiring_soon`・`unknown` の要対応への入り方を通す。呼び出す引数が固定の 2 つだけであること、出力にメール・`accountId` が出ないことも確かめる。同じスナップショットを固定して `ja`・`en`・`json` を描画し、収集器の結果が途中で変わっても出力が 1 文字も変わらないこと（FR-REP-7・10）をゴールデンファイルで確かめる。実際の `omp` に対する確認（新しい版があるときの出力の文言を含む）は実機で行い、未確認なら報告に明記する |
| NFR-TEST-18 | `make install`（`scripts/install.sh`）は、実際の GitHub にも systemd にも実際の `HOME` にも触れずに、`bash` のテスト（`scripts/install_test.sh`）で確かめる（`curl`・`systemctl`・`loginctl`・`uname` を PATH の先頭の偽物に、`HOME` をケースごとの一時ディレクトリに差し替える）：新規（設定なし → 有効にするだけ・案内して終了コード 0 ／ 設定あり → `enable --now`）、もう一度流しても同じ（バイナリの inode・unit・`daemon-reload`・`enable-linger` が変わらない）、新しい版への入れ替え（再起動しない）、入っている版が最新以上なら置き換えない、ハッシュ不一致・`SHA256SUMS` の行なし／複数行・版の不一致・アセット無し・最新の版の形が違う（どれも何も置かない。CRLF・`*` 付きは通る）、配置先がシンボリックリンク、unit がリンクや別内容のとき実ファイルで書き直す、非対応の arch、`active` にならないとき失敗、リンガリングがすでに `yes`。CI の `ci.yml` と `make check` で動かす。実際の systemd での `enable --now`・linger は CI で再現できないので、実機で確認し、未確認なら報告に明記する（NFR-TEST-10 と同じ扱い） |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-31 | 新規作成 | 初版 |
| 1.1 | 2026-11-01 | NFR-OPS-2 に `setup` を追加、NFR-OPS-8・9、NFR-TEST-12・13 を追加 | Issue #5 |
| 1.2 | 2026-11-01 | NFR-SEC-15・NFR-AVL-9・NFR-OPS-10・NFR-TEST-14 を追加、NFR-OPS-2 に `version --check` を追加 | Issue #7 |
| 1.3 | 2026-11-01 | 更新の方式を自己更新で確定：NFR-SEC-14・15 を改訂し NFR-SEC-16・NFR-AVL-10・NFR-TEST-16 を追加、NFR-AVL-8・9、NFR-OPS-1・10 を自己更新に合わせて改訂 | Issue #7（自己更新で確定） |
| 1.4 | 2026-11-01 | NFR-TEST-11 を自動タグ付きリリースに更新、NFR-TEST-15・NFR-OPS-11 を追加、NFR-AVL-8 を更新 | Issue #8 |
| 1.5 | 2026-11-01 | NFR-TEST-11 の成果物をアセット名 `swarm-console_vX.Y.Z_linux_amd64`・`SHA256SUMS` に、NFR-AVL-8 を自己更新（Issue #7）に合わせて更新 | Issue #8（Issue #7 の自己更新で確定） |
| 1.6 | 2026-11-01 | NFR-SEC-7 に引数固定の 2 コマンド（`omp usage --json`・`omp update --check`）の例外を追加、NFR-OPS-12・NFR-TEST-17 を追加 | Issue #21・#24（レポートの値） |
| 1.7 | 2026-11-01 | NFR-OPS-13・NFR-TEST-18（`make install`）を追加 | Issue #18 |
| 1.8 | 2026-11-02 | NFR-PERF-4：`pane_close` の応答時間の上限を、`shell` に戻るまでの待ちと画面確認の待ちの 2 つの `accept_timeout` を含む形にし、最初の待ちを超えたときは `exit_timeout`（`ok: false` ではない）と明記 | Issue #30 |
| 1.9 | 2026-11-02 | NFR-SEC-8：拒否するパスの設定を `denied_paths`（既定は `~/dotfiles` だけ。P-29）と明記し、存在しない「オーナーの checkout の一覧」設定を示す書き方をやめた。実装・設定の挙動は変えない | Issue #46 |
| 1.10 | 2026-11-02 | NFR-TEST-8 の CI のステップ一覧に linterly・release plan test・install script test を追加（`ci.yml` に合わせた） | Issue #41 |
| 1.11 | 2026-11-02 | NFR-SEC-1：tailnet の IP は 48120 以外のポートを起動拒否にし、ループバックだけどのポートでもよいと明記（P-73）。NFR-SEC-7：許可サブコマンドに `delete-buffer`（自分が作った一意名のバッファの後始末だけ）を追加（P-71）。NFR-PERF-3：「よくある入力での目標」と明記し、前提と単位（バイト）を書いた（P-74。実装は変えない） | Issue #72・#57・#71 |
