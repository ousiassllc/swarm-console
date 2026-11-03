# アーキテクチャ設計：提案した既定値と実装時確認事項（P-xx）

[アーキテクチャ設計](overview.md) の続き。§9 提案した既定値と実装時確認事項（`P-xx`）の一覧を定める。

改訂履歴は [アーキテクチャ設計（本書）](overview.md#改訂履歴) にまとめてある。

## 9. 提案した既定値と実装時確認事項

元仕様に書かれていない点を、実装に入るために埋めた既定値。オーナーの OK のときに合わせて確認する。「確認」列が「実装時」のものは、実環境・実画面を見ないと決められないもの。

| ID | 論点 | 提案 | 確認 |
|----|------|------|------|
| P-01 | 待ち受けアドレス | `listen_addr` を設定に明記。`0.0.0.0`・`::`・未指定は起動拒否。tailnet の範囲（`100.64.0.0/10`、`fd7a:115c:a1e0::/48`）かループバックのみ許可（ループバックはテスト用）。tailnet の IP のポートは 48120 だけ（P-73） | OK 時 |
| P-02 | MCP のセッション | Streamable HTTP はステートレスで動かす（`Mcp-Session-Id` を持たない）。SDK が対応していなければ、セッション ID を発行しても状態を持たない構成にする | 実装時（SDK の対応） |
| P-03 | `ops` トークンで `/report` | 呼べる（`ops` は `report` を含む）。`report` トークンは `/ops` で 403 | OK 時 |
| P-04 | トークンの形式 | `sc_` ＋ base64url 32 バイト。`tokens.toml` に SHA-256 を保存。失効は削除でなく `revoked_at` を付ける（名前の再利用は不可）。ファイルの更新時刻が変わるたびに再読み込み。発行時に 1 度だけ平文を標準出力へ | OK 時 |
| P-05 | スナップショットとサンプラー | §4 のとおり。`poll_interval` 5 秒、`snapshot_ttl` 2 秒。画面ハッシュは揮発部分を除いた本文で計算 | OK 時 |
| P-06 | エージェントの特定 | `/proc` でペインのプロセス木をたどり、コマンド名・引数がアダプタの `process_match` に当たるかで判定。取れなければ画面の特徴で補う。前面コマンドが shell のペインは、プロセス木が読めないときも、画面に残った過去の表示で誤判定せず `shell` とする（Issue #35）。代償：`/proc` が読めない環境で、シェルのラッパーが非 exec でエージェントを子として動かし前面コマンドが shell と報告されるペインは、画面にエージェントが出ていても `shell` と判定する | 実装時（実プロセス名） |
| P-07 | ウィンドウが複数のとき | ウィンドウが複数のセッションは `w<index>.` を付ける。段は `pane_top`、列は同じ段の `pane_left` の順位。固定名のキーは `<セッション>:<ウィンドウ>.<ペイン番号>` | OK 時 |
| P-08 | 並び変更の警告 | 検知から `layout_warning_ttl`（10 分）の間、`panes_status`・`report` に出す。起動直後は出さない | OK 時 |
| P-09 | 操作と状態の許可表 | 機能要件 §4.2 の「許可する状態」のとおり（`send_text` は `idle`・`working`・`stalled`、`asking` は拒否） | OK 時 |
| P-10 | `ask_pick` の `text` | `n` と `text` は同時に指定できない。`text` を指定すると「その他」を選んで `text` を入れる | OK 時 |
| P-11 | 報告待ちと最終報告ファイル | 報告待ち ＝ `idle` かつ最終報告ファイルがある。`dispatch`・`pane_close` は古いファイルを `reports/archive/<位置>-<unix 秒>.md` へ移す。位置の `:`・`/`・`\` はファイル名ではすべて `_` にする（`/`・`\` はパスの外へ出さないため。例 `a/b:T1` → `a_b_T1.md`）。アーカイブ名の `<位置>` も同じ規則（Issue #69 で実装がそろった） | OK 時 |
| P-12 | エラー形式 | 事前条件・権限の失敗は MCP の `isError: true` ＋ 本文 `{"error":{"code","message","details"}}`。送信後に期待どおりでない場合は `ok: false` ＋ `state`。`message` は日本語の固定文。HTTP の 401・403・413 も同じ本文形式 | OK 時 |
| P-13 | `send_text` の制御文字と `file` | 許可する制御文字は `\n`・`\t` のみ（ESC・NUL・CR など拒否）。`file` の読み取りは `file_roots`（`/tmp/` と作業フォルダの根）の下のみ、シンボリックリンク解決後に判定、UTF-8 で 8000 字以内 | OK 時 |
| P-14 | 秘密文字列のパターン | `gh[pousr]_`＋20 字以上、`github_pat_`、`sk-`＋16 字以上、`eyJ`＋20 字以上、`AKIA`＋16 字、PEM 見出し（`-----BEGIN ... PRIVATE KEY-----`）、自分のトークン（`sc_`＋40 字以上） | OK 時 |
| P-15 | メモの置き去り | tmux サーバーの起動時刻を `notes.json` に持ち、変わっていたら現行メモを履歴へ（`closed_by: "tmux_restart"`）。ペインが 2 回連続で消えていたら履歴へ（`vanished`） | OK 時 |
| P-16 | `agent_auth_status` の読み先 | **確定**（[agent-auth.md](agent-auth.md)）。Claude Code：`~/.claude/.credentials.json` の `claudeAiOauth.expiresAt`・`refreshTokenExpiresAt`（epoch ミリ秒）。**`expiresAt` が 0 なら `refreshTokenExpiresAt` が未来でも `expired`**（`0` は無期限ではなく失効の値）。`expiresAt` が過去（> 0）で更新が未来なら通常の自動更新として `valid`。判定表は agent-auth.md。Codex：`~/.codex/auth.json` の `tokens.access_token`（JWT）の `exp`（秒）。omp：`[agents.omp] auth_file`（未設定なら「不明」）。読むのは期限だけ | **確定**（実機の Claude は `expiresAt: 0`＋トークン空で `expired`） |
| P-17 | 数量と文字数の上限 | 総ペイン 32、文字列項目 2000 字（`memo` 4000 字）、`items`・`rules` 50 件、`note_log` 1 行 500 字、リクエスト本文 256 KiB、`pane_tail` 20,000 字、`report_read` 20,000 字 | OK 時 |
| P-18 | 指示文の形 | `command` → 空行 → 「## やること」`task` → 空行 → 「## 条件」の箇条書き（共通 → プロファイル → `extra_rules`）。`{report_path}` は最終報告ファイルの実パスに置換 | OK 時 |
| P-19 | `pane_close` の許可状態 | `idle`・`shell` のみ。`working`・`stalled`・`asking`・`unknown` は拒否（元仕様は `working` のみ明記） | OK 時 |
| P-20 | リリース | タグ `v*` の push で linux/amd64 バイナリを GitHub Releases に載せるワークフロー。P-48 で自動のタグ付けを足した | OK 時 |
| P-21 | テスト用の tmux | `tmux_socket`（`-L`）で専用サーバーを使う | OK 時 |
| P-22 | 状態判定・カードの正規表現の具体値 | [agent-patterns.md](agent-patterns.md) のとおり。既定値は組み込み、`[agents.<name>]` で上書き可。**omp は実画面で確定**。Claude Code は idle・信頼確認・`/model` が実画面、作業中・許可確認・質問は推定（ログイン切れで採れず）。**Codex は実画面で未確定**（この機に `codex` が無い。推定値） | **未完了**：Claude Code の作業中・許可確認・質問、Codex の全画面は、実画面で確かめて値を直す |
| P-23 | `dispatch` の `note` 保存 | 送信が行われた後に保存（拒否・事前検証失敗では保存しない）。受理に失敗しても保存し `note_saved: true` を返す | OK 時 |
| P-24 | tmux のバージョン | **確定**：3.x を前提。実機は `tmux 3.2a`（`load-buffer -`・`paste-buffer -p` の実動作を専用ソケットのテストで確認）。`tmux -V` を起動時に読み、`tmux.CheckVersion` が 2.x 以下・判定不能なら警告（3.x・`next-`・`master` は警告なし） | 実機で確定済み |
| P-25 | `report` の `format: json` と `pane` の併用 | `panes` と `attention` をその 1 ペインだけに絞る。`host`・`layout`・`warnings` は全体のまま | 実装時に決めた。OK 時に確認 |
| P-26 | `/report` で操作ツールを `tools/call` したとき | JSON-RPC `-32601` ではなく、`isError: true` ＋本文 `{"error":{"code":"unknown_tool",...}}` で返す（go-sdk v1.8.0 が `-32601` の `data` を捨てるため）。何も実行しない | 実装時に決めた。OK 時に確認 |
| P-27 | `layout`（段・列の数）が複数セッションのとき | 既定セッション（無ければセッション順で最初にペインのあるもの）の最初のウィンドウ。`session` で 1 つのセッションに絞った応答は、そのセッションの最初のウィンドウから計算し直す（P-82） | 実装時に決めた。OK 時に確認 |
| P-28 | `unknown`（エージェント無し）のペインへの操作 | `ask_pick`・`send_text`・`send_escape`・`pane_close`・`dispatch` は `invalid_state` ではなく `unsupported_agent`（API 仕様 §3.2 の表に合わせる） | 実装時に決めた |
| P-29 | `denied_paths` の既定 | **確定：既定 `~/dotfiles`、`config.toml` で変更可**。`denied_paths` を書いたら既定と入れ替わる（足されない）。書かなければ既定。`~` は展開して絶対パスで比較。`cwd`・`file` の両方に効く。オーナーの checkout の一覧は仕様書に無いため、必要なら設定で書く（例は [環境構築](../environment/setup.md#ローカルで動かす)） | **確定** |
| P-30 | `report_read` の読み取り | 最終報告ファイルは `O_NOFOLLOW` で開く（シンボリックリンクなら `exists: false`）。認証ファイルなどを読み出されないため。有無の判定（`report_exists`・1 ペインの詳しい版の最終報告欄・アーカイブ先の重複判定）も `lstat` でリンクを辿らず、リンクは「無し」 | 実装時に決めた |
| P-31 | 仕様書の行数管理 | **確定**：仕様書（`docs/`）も Linterly の対象にする。当初は `api/endpoints.md`・`architecture/er.md` が 300 行を超えたため `.linterlyignore` で `docs/**` を外していたが、Issue #3 で除外をやめ、超えたファイルを責務ごとに分割した（1 ファイル 300 行以内、警告の閾値 290 行以内） | **確定** |
| P-32 | `setup` のコマンド名と起動 | **`swarm-console setup [--config <path>]`**。ファイルが無ければ新規、あれば読み込んで編集（初回専用にしない）。サブコマンド名は Issue の「仮」を確定 | 実装時に決めた。OK 時に確認 |
| P-33 | tailnet IP の候補 | `net.InterfaceAddrs` を `100.64.0.0/10`・`fd7a:115c:a1e0::/48` で絞り、ポート `48120` を付けて選択肢にする。`tailscale ip` は実行しない（外部コマンドなし）。0 件なら手入力だけ | 実装時に決めた |
| P-34 | 書き戻し方式 | 変えた値のバイト範囲だけ置き換える（コメント・並び・空行・書式は保つ）。コメント入りの複数行配列を変えるときだけ、中のコメントが消えることを確認する。全体の再出力（`Marshal`）は採らない | OK 時に確認 |
| P-35 | `config.toml` がシンボリックリンク | 書かずに中止（rename でリンクが普通のファイルに変わる。リンク先が置き場の外・`~/dotfiles` のことがある。NFR-SEC-14）。手で編集してもらう | 実装時に決めた |
| P-36 | TUI のライブラリ | `charmbracelet/huh`（Bubble Tea 上）。バージョンは実装時に最新の安定版を `go.mod` に入れる。画面は実 tmux のペインで確かめる | OK 時に確認 |
| P-37 | `setup` の対象外 | `profiles.toml`・レポートのテンプレート・`setup` が聞かないキー（FR-SET-4）は手書きのまま。`--non-interactive`（質問なしで値を渡す）は作らない（TTY でなければエラー。FR-SET-6） | 実装時に決めた |
| P-38 | 既存ファイルが壊れているとき | TOML 構文エラー・未知キーは開かず終了（自動で直さない）。値の検証エラー・`listen_addr` 未設定は開いて直させる | 実装時に決めた |
| P-39 | 新規作成のファイル権限 | 600（「644 以下」を満たす）。既存ファイルの権限は変えない | 実装時に決めた |
| P-40 | 新しい版の確認の入口 | **確定（Issue #7 のヒアリングで選択）**：`swarm-console version --check`・`serve` の定期確認・`host_status` の `update` の 3 つ全部。`update` サブコマンドは作らない（入れ替えのきっかけは `serve` の自動だけ。P-46） | **確定** |
| P-41 | 定期確認の既定 | **確定（Issue #7 のヒアリングで選択）**：既定オン・24 時間ごと。`config.toml` の `update_check = false` で止める。設定なしで GitHub へ外向き通信が出る（確認の `api.github.com` の GET 1 本と、自己更新のダウンロード。NFR-SEC-15） | **確定** |
| P-42 | 版の形・比較・プレリリース | タグは `vX.Y.Z` だけを受け付ける（Issue #8 が付ける形と同じ）。比較は `golang.org/x/mod/semver`。`/releases/latest` を使うのでプレリリース・ドラフトは通知しない。現在の版がプレリリース形でも semver どおりに比較する | 実装時に決めた。OK 時に確認 |
| P-43 | `version --check` の終了コード | 最新 0・新しい版あり 10・確認できない 1。スクリプトで 3 つを区別できるようにする | 実装時に決めた |
| P-44 | 確認の失敗の扱い | 失敗後の再試行は次の周期まで行わない（バックオフなし）。ログは新しい版を見つけたとき 1 回と、失敗の `reason` が変わったときの警告だけ（FR-UPD-8）。`serve` は止めない | 実装時に決めた |
| P-45 | 最初の確認の遅れ | 起動の 1 分後（起動処理・サンプラーの初回取得と重ねない。`clock` 経由） | 実装時に決めた |
| P-46 | 更新の方式 | **確定（Issue #7 のヒアリングで選択）：自己更新。** 検証は `SHA256SUMS` のハッシュだけ（署名なし）、きっかけは `serve` の自動、再起動は自分で正常終了して systemd に立ち上げ直してもらう。限界とリスクは機能要件 §12.5 | **確定** |
| P-47 | 確認に認証を使わない | リポジトリが public である前提（public になった。private に戻すと GitHub は `releases/latest` に 404 を返し、`no_release` になる）。トークンは送らない（NFR-SEC-15） | 実装時に決めた |
| P-48 | 自動リリースのきっかけ | mam の `release.yml` に合わせる：`ci.yml` の `workflow_run`（main への push の CI が成功）で、main の先頭のコミットに次の版を付けてリリースする。古いコミット・PR・フォークの CI は対象外。手で打ったタグ `v*` の push も同じワークフローが受ける（マイナー・メジャーを上げる） | OK 時に確認 |
| P-49 | タグを付ける方法 | `GITHUB_TOKEN` で push したタグは他のワークフローを起動しないので、同じ `release.yml` の中で `gh release create --target <コミット>` によりリリースとタグを同時に作る。タグ専用の別ワークフロー・PAT・GitHub App は使わない（秘密を増やさない）。ビルドが落ちればタグは作られない | 実装時に決めた |
| P-50 | 版の上げ方 | **確定（Issue #8 のヒアリングで選択）**：毎回パッチを上げる（mam と同じ）。コミットメッセージ（`feat:`・`fix:`）は見ない。タグなしなら `v0.1.0`。docs だけの変更でも版が進む。この前提は、マージコミット方式（squash しない）でもコミットの書き方に依らないこと | **確定** |
| P-51 | 同時実行とキャンセル | `concurrency: group: release`・`cancel-in-progress: false`。GitHub は待ちを 1 つしか持てず古い待ちをキャンセルする。main の古いコミットのリリースが落ちるのは設計どおり。手で打ったタグの実行が落ちたら再実行する | 実装時に決めた |
| P-52 | 変更履歴 | `gh release create --generate-notes` だけ。`CHANGELOG.md` は作らず、マージ（PR）の一覧が GitHub のリリースノートになる。コミットが PR 経由でないと一覧が薄くなる | 実装時に決めた。OK 時に確認 |
| P-53 | 手で打ったタグの検査 | 既存の他の `vX.Y.Z` より大きいこと・main の履歴にあり CI が成功していること。GitHub の「最新のリリース」が日時順で決まり、版が戻ると Issue #7 の確認が古い版を「最新」と読むため。違えばエラーにしてリリースしない（タグは自動では消さない。リモートのタグも消さない方針） | 実装時に決めた。OK 時に確認 |
| P-54 | 作業中のペインがあるときの更新 | **確定（Issue #7 の回答）**：待たずにすぐ入れ替えて終了する。ペインは tmux に残り、MCP クライアントはつなぎ直す。進行中のツール呼び出しは NFR-AVL-7 の 10 秒で打ち切る（mam の `SafeGate` のように作業中は更新しない、は採らない） | **確定** |
| P-55 | 起動失敗の判定と戻し | 戻すこと自体は **確定（Issue #7 の回答）**。判定と再起動ループの防ぎ方は実装案：入れ替え時に `.old`（ハードリンク）と `update-pending`（`boots` 0）を置く → 新バイナリは起動の最初に `boots` を +1 し、**3 回目の起動**で `.old` を戻して終了、`update-failed` に版を記録して同じ版を再試行しない（FR-UPD-17）。閾値 3 は `RestartSec=2` で約 4 秒後 ＝ systemd の起動回数の上限より前。60 秒生きれば確定。bind 失敗（tailscale 未起動）も数える＝旧バイナリでも起きる環境要因と区別しないが、戻したあとは `update-failed` でその版を止めるので、無限には繰り返さない。コードを 1 行も実行できない故障は数えられない（§12.5） | 実装時に決めた。OK 時に確認 |
| P-56 | `update_auto` の既定 | 運用者の回答は「自動更新を止める設定は `update_auto` を新しく足す」。「止める設定」と読み、**既定 `true`（自動で入れ替わる）、`false` で確認だけ**とした。`update_check = false` なら無効。ハッシュだけの検証と自動の組み合わせなので、既定を `false` にして明示的に有効にする案もある | **OK 時に確認（既定値）** |
| P-57 | ダウンロード先・アセット名・`SHA256SUMS` の書式 | アセット名は `swarm-console_vX.Y.Z_linux_amd64`（**確定**：版・OS・arch を含める）。`SHA256SUMS` は `sha256sum` の出力どおり（`<ハッシュ>␣␣<アセット名>`）の 1 行。ダウンロード URL は検証済みの `tag_name` から `releases/download/<tag>/…` を組み立て、API 応答の `assets[]` は使わない（応答の文字列を URL にしない）。許可ホストは `github.com`・`objects.githubusercontent.com`・`release-assets.githubusercontent.com`。サイズ上限：`SHA256SUMS` 64 KiB・バイナリ 64 MiB。生産側（`release.yml`）の改訂は Issue #8 | 実装時に決めた。OK 時に確認 |
| P-58 | 置き場と入れ替えの方法 | 置き場は `~/bin/swarm-console/` に**固定**（**確定**。`os.Executable()` が違えば `not_managed`）。入れ替えは「`.new` に書く → `.old` へハードリンク → マーカー → `.new` を `rename`」で、本体の名前が一度も消えない（途中で落ちても `swarm-console` がある）。旧バイナリは 1 世代だけ | 実装時に決めた。OK 時に確認 |
| P-59 | 試運転中に見送った入れ替えの再試行 | 試運転中（`apply = probation`）に当たった `update_available` は入れ替えず（FR-UPD-12。Issue #12）、見送った最新版を覚える。試運転が確定した時点で、次の周期（既定 24 時間後）を待たずに、その版へ入れ替えを試す（確認の通信はし直さない）。その後の確認が `up_to_date` なら取り下げる。確認と確定は別の goroutine なので、入れ替えは `serve` 内で 1 本ずつ行う | 実装時に決めた（Issue #15）。OK 時に確認 |
| P-60 | 使用状況・omp の版の取り方（Issue #21・#24） | **確定（Issue #21〜#25 のヒアリングで選択）**：`omp usage --json` と `omp update --check` を `serve` が引数固定で実行する（NFR-SEC-7 を 2 コマンドだけ許すよう改訂）。プロバイダの API を直接呼ぶ案（トークンファイルを読む）は秘密の扱いと NFR-SEC-15 を大きく変えるので採らない。取り方は [report-values.md](report-values.md) | OK 時に確認（`omp` の `--json` の形は omp の版で変わりうる） |
| P-61 | Claude 5 アカウントの扱い（Issue #21） | 返ったアカウントを全部出す。枠が取れないアカウント（5 時間枠が返らないアカウント）は枠を `available: false` にして残す。`omp` の `accountsWithoutUsage` も同じ扱い | 実装時に決めた。OK 時に確認 |
| P-62 | 使用状況の並び（Issue #21） | `account` の昇順。前のスクリプトは使用率の降順だったが、取得ごとに並びが入れ替わらないようにした（FR-REP-7 の出力を安定させる）。使用率の大きい順に見たいときは JSON を使う側で並べ替える | OK 時に確認 |
| P-63 | SSD 2 台と CPU・スワップ（Issue #23） | **確定（ヒアリングで選択）**：2 台は既存の `disk_mounts`（既定 `/`・`/mnt/nvme`）、値は容量の使用率（`statfs`）。前のスクリプトのバーは I/O の忙しさ（`iostat`）だったが採らない（Issue の「使用量」の字面どおり）。CPU は `/proc/stat` の差（サンプラーが持つ）、スワップは `/proc/meminfo`。バーの書式・文字数・使う文字は仕様に入れない（値だけを返す） | 確定 |
| P-64 | omp の更新確認の読み方（Issue #24） | 出力の `Current version: X.Y.Z`・`New version available: X.Y.Z`・`Already up to date` を読む。最新のとき（`Already up to date`）と、更新ありのとき（`Current version: 18.14.2` の次の行に `New version available: 18.15.0`。ANSI 制御文字なし）の両方を、2026-11-02 にこの機の実際の `omp update --check`（omp 18.14.2）で確認した。間隔・設定は swarm-console の確認（`update_check`・`update_check_interval`）と同じ | 確定（実機の出力で確認済み） |
| P-65 | swarm-console 自身の更新状態も出すか（Issue #24） | 出す。`report` の JSON に `update`（`host_status.update` と同じ）として載せ、markdown の見出しにも 1 行。追加の通信・設定は無い。`host_status` と `report` が同じスナップショットの値を返すので、FR-UPD-9 の「`report` には載せない」を改めた（描画はスナップショットの値だけで、AC-1・NFR-PERF-3 を保つ） | **確定（実装時の確認で選択）** |
| P-66 | ログイン期限の警告（Issue #25） | **確定（ヒアリングで選択）**：期限切れと 48 時間以内（既存の `expiring_soon`）だけ要対応の一覧に入れる。`unknown` は要対応に入れず、`auth` に `unknown` のまま残し、markdown の見出しに「不明」と出す。閾値は増やさない | 確定 |
| P-67 | ペインの経過・markdown に載せるか（Issue #22・#21〜#25） | **確定（ヒアリングで選択）**：経過は既存の `elapsed`（作業が始まってから）をそのまま使い、表の「状態」の右に列を足す。新しい値は markdown にも載せる（FR-REP-2・§5.3・テンプレートを改訂）。ただし見た目はバー・色分け・警告記号を使わず値だけ | 確定 |
| P-68 | `pane` 指定の JSON でのログインの要対応（Issue #25） | **確定（実装時の確認で選択）**：`pane` を指定しても `auth_expired`・`auth_expiring`・`usage`・`omp`・`update`・`auth`・`host` は絞らず常に入れる（ペインに属さない。markdown の詳しい版が見出し・使用状況を全体と同じに出すのと同じ）。`panes` とペインの項目だけ絞る。並びはペインの項目のあとにログイン | 確定 |
| P-69 | 英語の「あと N 時間」（Issue #25） | **確定（実装時の確認で選択）**：`in 5 min`・`in 10 h 5 min`・`in 2 d 3 h`（単位は ja に 1 対 1：`min`・`h`・`d`。区切りの規則は ja と同じ） | 確定 |
| P-70 | `pane_close` が終了コマンドのあと待つ方法（Issue #30） | 終了コマンドのあと、ペインの状態が `shell` に戻るまで（スナップショットを `refreshStep` = 500 ms ごとに取り直し、状態の判定は `panes_status` と同じ）待ってから `cd ~` 以降を送る。上限は新しい設定を増やさず既存の `accept_timeout`（既定 10 秒）。超えたら `cd ~` 以降を送らずエラー `exit_timeout`（FR-O-C2 の「`ok: false`」の例外。メモ・最終報告は移さず `note_archived` は `false`）。`shell` のペインは今までどおり終了コマンドを飛ばす | 実装時に決めた |
| P-71 | `Paste` が失敗したあとのバッファ（Issue #57） | 許可サブコマンドに `delete-buffer` を足す（NFR-SEC-7・overview・components も同じ）。`Paste` は `load-buffer` か `paste-buffer` が失敗したとき（ctx の中断・タイムアウトを含む。中断済みの ctx でも消せるよう後始末は中断を引き継がない）、自分が作った一意名のバッファだけを `delete-buffer -b <名前>` で消す。ほかのバッファは触らない。残す案・`send-keys -l` で貼る案（bracketed paste が使えなくなる）は採らない | 決定済み（Issue #57） |
| P-72 | `panes_status` の `truncated`（Issue #70） | 実装を API 仕様 §4・§7 に合わせる。`panes_status` と `report`（json）の `panes[]` に、`activity` を 120 字で切ったときだけ `"truncated": true` を付ける（`omitempty`。キーは `note` の次で、既存のキー順は変えない）。仕様は直さない | 決定済み（Issue #70） |
| P-73 | `listen_addr` のポート（Issue #72） | 実装を NFR-SEC-1 に合わせる。tailnet の IP は 48120 以外のポートで起動拒否。ループバックはどのポートでもよい（ポート 0 も）。`setup` の候補は今までどおり 48120。48120 以外のポートで運用している既存の `config.toml` は起動できなくなる（運用ホストの設定は触らず、PR で報告する） | 決定済み（Issue #72） |
| P-74 | NFR-PERF-3 の意味（Issue #71） | 「よくある入力での目標」と明記し、前提（8 ペイン・短いメモ・数個の選択肢）と単位（バイト）を書く。コードで上限は保証しない（API 仕様 §7 の上限いっぱいの入力は対象外）。コード・返り値は変えない | 決定済み（Issue #71） |
| P-75 | `make check` の build（Issue #40） | `make check` の先頭を CI の `Build` ステップと同じ `go build ./...` にする。`make build`（`bin/swarm-console` を作る）は残すが、`check` は呼ばない | 決定済み（Issue #40） |
| P-76 | トークンごとの許可セッションの置き場（セッション分割） | **確定（オーナーの回答）**：`tokens.toml` の各 `[[token]]` に `allowed_sessions`（string 配列、任意）。`config.toml` 側に別の表は作らない（トークン名の二重管理になる）。省略は `config.toml` の `sessions` 全部（後方互換）、空配列・重複・不正な文字は読み込みで拒否。`serve` の起動時に `sessions` にない名前があれば起動拒否（エラーにトークン名とセッション名。ハッシュは出さない）。起動後の再読み込みで食い違ったときは警告ログだけで、実効の許可は `sessions` との共通部分（空になりうる）。`auth.Client.AllowedSessions` は `nil` が全部、値があれば明示のリストで、ツールは常に `sessions` と共通部分を取る | **確定** |
| P-77 | `session` の省略規則と `panes_status` の変更 | **確定（オーナーの回答）**：許可がちょうど 1 つなら省略はそれ、2 つ以上（または 0）は `session_required`（`details` は `allowed_count` だけで、許可の一覧は出さない）。許可セッションを黙って合算する案・`default_session` を使う案は、ACL を入れたあとに「見えてはいけないペイン」が混ざる・省略の意味が設定で変わるので採らない。`panes_status` の「省略は全セッション」はこの規則に置き換わる（許可が 2 つ以上のトークンには破壊的変更で、毎回 `session` を付ける）。`sessions = ["0"]` だけ・`allowed_sessions` なしは今までどおり引数なしで動く | **確定** |
| P-78 | 許可セッションの変え方 | **確定（オーナーの回答）**：`token revoke` してから `token add <名前> --scope ops|report --sessions 0,1` で発行し直す。属性を更新するサブコマンド（`token set-sessions` など）は作らない。`tokens.toml` の手編集（権限 600・未知キー拒否の範囲内）は動くが正式な手順ではない。`--sessions` はカンマ区切りで、省略ならキーを書かない。`--sessions` があるときだけ `config.toml` を読み、読めない・検証できない・`sessions` にない名前なら発行しない | **確定** |
| P-79 | 許可外のセッションのペイン | 読み取りも操作も `session_forbidden`（他のセッションの指示・cwd・メモの漏洩を防ぐ）。ホスト全体の値（負荷・メモリ・ディスク・`usage`・`omp`・`update`・ログイン期限）だけは許可に関係なく見える。`pane_not_found` は「許可されていてセッションの中で解決できない」ときだけ。許可外のペインの中身（位置・タイトル・cwd・メモ）は応答に出さない。ただし tmux ID・全体の名前（`1:T1`・固定名）で直接指すと、実在すれば `session_forbidden`（`details.session` はそのセッション名）、無ければ `pane_not_found` なので、許可外のペインの有無はこの 2 つで分かる（ID の存在を隠せないための限界。`sessions_list` の `all: true` もセッション名は見せる） | 実装時に決めた。OK 時に確認 |
| P-80 | `sessions_list` の `all` と `operable` | 既定は許可セッションだけ。`all: true` で `sessions` 全部を返し、許可外はスタブ（`id`・`allowed: false`・`operable: false`。`layout`・`panes` なし）にして、ペインの中身は漏らさない。完全に隠す案は、依頼文を書く側が「相手のセッション名」を知れなくなるので採らない。`operable` は許可内・スコープ `ops`・`/ops` 経由のときだけ `true`。窓口は `report` スコープ（`/report`・`/ops` の両方）で、ツール数は /report 10・/ops 18 | 実装時に決めた。OK 時に確認 |
| P-81 | セッションの中でのペイン解決 | 候補は決めたセッション S のペインだけ。当たる指定は tmux ID・S 内の位置（接頭辞なし。大文字小文字を区別しない）・全体の名前（`1:T1`・`pane_names` の固定名）・`<S>:<S 内の位置>`。S に無く別のセッションのペインに当たる（tmux ID・全体の名前）ときは、許可外なら `session_forbidden`、許可内で S と違えば `invalid_args`（`details.field` = `"pane"`。自動で別のセッションに切り替えない）。それ以外が `pane_not_found` | 実装時に決めた。OK 時に確認 |
| P-82 | セッション内の位置・`layout`・最終報告ファイル名 | 1 つのセッションに絞った応答の位置は接頭辞なしのセッション内の位置（命名アルゴリズム FR-POS-2 は変えない。3 列×2 段は `T1`〜`T3`・`B1`〜`B3`、4 列×2 段は `T1`〜`T4`・`B1`〜`B4` のまま）、`layout` はそのセッションのペインから再計算する（以前の `panes_status` は絞ったときも `layout` を計算し直していなかった）。最終報告ファイル名だけは全体の位置から付ける（既定セッション `T1.md`、それ以外 `1:T1` → `1_T1.md`）。セッション内の位置で付けると、別のセッションの `T1` が同じファイルに衝突するため。`dispatch` の `{report_path}` と `report_read` も同じパス | 実装時に決めた。OK 時に確認 |
| P-83 | `host_status`・`report` の `session` | ホスト全体の値（`host_status` の負荷・CPU・メモリ・スワップ・ディスク・稼働時間・`usage`・`omp`・`update`、`report` の `host`・`usage`・`omp`・`update`・`auth`・ログインの要対応）は絞らない。ペインに関する値だけ絞る（`host_status` は `agents` の数と `tmux_session`、`report` は `panes`・`layout`・`attention` のペインの項目・`warnings`）。共有ホストの健全性確認は許可に関係なく見える | 実装時に決めた。OK 時に確認 |
| P-84 | `warnings` の絞り方 | セッションに絞った応答には、そのセッションの分だけ入れる：`layout_changed`（セッションごとに検知。メッセージの位置はセッション内の位置）・`pane_unreadable`（そのセッションのペイン）・`session_missing`（欠けているセッションごとに 1 件。メッセージ `tmux セッションが見つかりません: <名前>`）。`tmux_unavailable` は全体の警告なので常に入れる。他のセッションの並びの変化を見せない | 実装時に決めた |
| P-85 | resources の許可と識別子 | `resources/list`・`read` は許可セッションのペインだけ。`swarm://pane/<位置>`・`swarm://notes/<位置>` の `<位置>`：許可が 1 つなら S 内の位置か tmux ID（今までどおり）、2 つ以上ならセッション内の位置は曖昧になるので全体の名前（既定セッション `T1`、それ以外 `1:T1`。全体の名前は一意）か tmux ID で、list も全体の名前で載せる。`swarm://report` は許可が 1 つのときだけそのセッションの report、2 つ以上なら読めず（`session_required`）list にも載せない。JSON-RPC のエラー：`pane_not_found`・`session_forbidden` は `-32002`、`session_required` は `-32602`（どれも `data` に API 仕様 §3.1 の本文） | 実装時に決めた。OK 時に確認 |
| P-86 | `dispatch` の prompt の `session` | 任意の引数を先頭に足し、描画する JSON の先頭にも入れる（説明：許可が 1 つなら省略、そうでなければ必須）。ドラフトの「必須引数」は採らない（必須にすると、許可が 1 つの今までのクライアントが prompt を使えなくなる）。`report` の prompt は変えない | 実装時に決めた。OK 時に確認 |
| P-87 | `default_session` の役割 | 位置の接頭辞を省略するセッション（FR-POS-1・3）だけ。`session` の既定にもツールの許可にも使わない（設定の書き換えで、許可や省略の意味が静かに変わらないようにする） | 確定 |
| P-88 | 監査ログの `session` | `pane` の隣に任意の `session`（渡された `session` 引数をそのまま。40 字で切る。`dispatch` の prompt は prompt の `session` 引数）を記録する。トークン・本文は記録しない（NFR-SEC-12） | 実装時に決めた |
| P-89 | `session` を取らないツールと既知の限界 | `note_history`（閉じたペインのメモ）と `agent_auth_status`（ホストのログイン期限）は `session` を取らない。履歴のメモはペインのセッションで絞らないので、どの許可のトークンにも全セッション分の履歴（`pos`・`title`・`from`・`origin`・`started_by`・`repo`・`log`）が見える。履歴にセッションを持たせて絞るのはこの変更の範囲外（必要なら別の Issue） | 実装時に決めた。OK 時に確認 |
| P-90 | 既知の限界：ペインが別のセッションへ移るとき | 許可の検査・ペインの解決はスナップショット（`snapshot_ttl` 既定 2 秒）に基づく。2 秒以内に `move-pane`・`join-pane` でペインが別のセッションへ移ると、許可を確かめたときと違うセッションのペインに操作が当たりうる。ペインの移動は運用側が行うもので、この変更では対策しない | 実装時に決めた。OK 時に確認 |
