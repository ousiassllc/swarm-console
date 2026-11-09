# アダプタの判定規則と P-22 の確定値

[アーキテクチャ設計 §9](decisions.md#9-提案した既定値と実装時確認事項) の P-22（状態判定・カードの正規表現の具体値）に対応する。
値は各アダプタの `defaults`（`adapter.go`）にある。`[agents.<name>]` で上書きできる（FR-AD-2）。
書かれたキーだけが置き換わる。`command`・`exit_command`・`auth_file` の空文字は設定エラー（起動拒否）で、ほかのキーの空文字・空配列は「上書きしない」。正規表現が不正なら Factory がエラーを返す。

| 確かさ | omp | claude | codex |
|--------|-----|--------|-------|
| 画面の出どころ | 実画面（omp v18.14.1、120×40） | idle・信頼確認・`/model` メニューは実画面（v2.1.316）。作業中・許可確認・質問は**推定**（ログイン切れで採れず。Issue #4 で再度試したが同じ `Login expired` で採れなかった） | idle・作業中・コマンド実行の承認カード（`touch`）・承認後の idle は**実画面**（codex-cli 0.167.0、120×40。Issue #4）。ファイル編集の承認カード（`patch-1.txt`）・`/quit` は**推定のまま** |
| サンプル | `testdata/screens/omp/` | `testdata/screens/claude/`（推定は先頭行に `# 実画面ではない（推定）`） | `testdata/screens/codex/`（`patch-1.txt` だけ先頭行に推定の注記。ほかは実画面） |

## 既定値（Patterns）

| キー | omp（実画面で確定） | claude | codex（実画面で確定。`patch-1` の編集カードと `/quit` は推定） |
|------|--------------------|--------|---------------|
| `process_match` | `(^|/)omp$`（実機の argv[0] は `omp`） | `(^|/)claude$`（実機の argv[0] は `claude`。実行ファイルは `~/.local/share/claude/versions/<ver>` へのリンクで、cmdline は `claude --permission-mode default`） | `(^|/)codex$` |
| `working_re` | `^\x{e0b6}\s*[\x{2800}-\x{28ff}]`（ステータス行の左端の直後に点字スピナー） | `esc to interrupt`（推定） | `esc to interrupt`（実画面の `• Working (0s • esc to interrupt)`） |
| `ask_re` | `^╭─ (?:Ask\|Custom answer)\b` | `(?i)\b(?:Esc to cancel\|Enter to (?:select\|confirm\|set)\|Do you want to\|Would you like to)\b`。信頼確認・`/model` は実画面の `Enter to confirm`・`Enter to set as default`。許可確認・質問は推定 | `(?i)(?:Press enter to confirm or esc to cancel\|Would you like to\|Do you want to)`（実画面の `Would you like to run the following command?`・`Press enter to confirm or esc to cancel`） |
| `idle_re` | `^\x{e0b6}`（ステータス行があり、作業中でも質問でもない） | `^\s*[❯>](?:[\s\x{a0}]\|$)`（入力欄。上下を罫線 `─` が挟む。実画面） | `^\s*›(?:[\s\x{a0}]\|$)`（入力欄 `› Ask Codex to do anything`。実画面） |
| `status_line_re` | `^\x{e0b6}`（ステータス行） | `^\s*(?:[⏵⏸]\|\? for shortcuts\|esc to interrupt\b\|Not logged in\b)`（実画面＋推定） | `^\s*(?:\d+% context left\|(?:←[^·]*·\s*)?\? for shortcuts\|Ctrl\+C again to quit)`（実画面の下端は `←  for agents · ? for shortcuts`。**`N% context left` は実画面に出なかった**ので推定のまま残す） |
| `border_re` | `^\s*[╭├╰]─` | `^\s*(?:[─━▔]{3,}\|[╭├╰]─)` | `^\s*[╭╰]─` |
| `spinner_chars` | `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`（実画面） | `✻✢✳✶✽✦·*`（`✻` は実画面の `Cogitated for` で確認、他は推定） | `•◦`（`•` は実画面の `• Working`・`• Running` で確認。`◦` は推定） |
| 起動・終了 | `omp`・`/exit`（実機で `/exit` + Enter により `Closing session…` が出てプロセスが終わり、ペインがシェルに戻るのを確認。omp v18.15.0） | `claude`・`/exit`（実機で `/exit` + Enter によりプロセスが終わるのを確認） | `codex`・`/quit`（`codex` は実機で起動を確認。`/quit` は推定） |

omp のステータス行は Nerd Font の私用領域の文字（`U+E0B6` 左端、`U+E0B1` 区切り、`U+F054` カーソル、`U+F10C` など）を使う。
`capture-pane -p` にはこれらがそのまま出る。ステータス行は `U+E0B6 <スピナー> <経過> U+E0B1 …`
の形で、スピナーと経過（`36s`・`2m`・`4h`）は作業中のときだけ入る。

## 判定の規則（どの目印で何を判定するか）

共通の順序は「質問カード → 作業中 → 入力待ち → どれでもなければ `unknown`」。`shell`・`stalled` は `pane` が決める。

### omp

| 判定 | 目印 | 誤判定しやすい点 |
|------|------|------------------|
| asking | 画面の最後の行が `╰──`、その枠の上枠が `╭─ Ask`（選択）または `╭─ Custom answer: …`（自由入力中） | 回答後に残る結果の枠は `╭─── Ask …`（`─` が 3 つ以上＋記号）で、**その後ろにステータス行が続く**ので当たらない。「枠の最後の行が画面の最後」という条件が肝 |
| working | ステータス行（下から 8 行以内の `U+E0B6` 行）のスピナー位置に点字文字 | 本文に `⠋` などが出ても、ステータス行の位置を固定しているので影響しない |
| idle | ステータス行があり、上の 2 つに当たらない | 起動直後（ロゴの画面）も idle。スピナーも経過もない |
| Activity | ステータス行の直上の行（`<記号> Working…`・`<記号> Asking fruit choice`）の文言。右寄せのタイトル（`U+F1042 FRUIT: …`）は字下げが大きいので除く | 活動行がないとき（`You chose …` が直上に来るなど）は空 |
| Elapsed | ステータス行の最初の区切り `U+E0B1` より左の `\d+(s\|m\|h)` | 時間が長いと単位が粗くなる（`2s`→`4m`→`4h`）。分・秒の混在はない |
| カード | 見出し行＝質問文。選択肢は `│ [U+F054] <記号> ラベル │`。`(Recommended)` は Recommended=true で文言から外す。`Other (type your own)` は Options に入れず AllowsCustom=true（カーソルがそこなら Cursor=len(Options)+1） | 複数選択（`U+F096`/`U+F14A`、見出しの 1 行目に `Submit` のタブ）も同じ形。`Custom answer` の枠は選ぶ対象がないので ParseCard は ok=false |
| キー | `PickKeys` は Up/Down の差分だけ、`CustomKeys` は「その他」まで Down。Enter は含めない。カーソルが読めないときは `cursor_mismatch`。キーを送った直後の画面は描き直し前のことがあるので、呼び出し側（`ask_pick`）が反映を待ってから検証する | 数字キーは使わない（フッターは `↑/↓ move`・`select` のみ） |
| 自由入力 | 「その他」で Enter → `╭─ Custom answer: <質問>` の枠（`> ` の行へ入力）。Enter で送信（フッターには `Ctrl+Q submit` もある） | 送信後は `Ask` の結果枠にチェック付きで本文が出て、そのまま作業中に戻る |
| Clean | 画面下のステータス行・活動行・タイトル行、`╭╰├` の枠線、`Todo N tasks` の枠、`TODO` の木、枠の左右の `│` を除く | TODO は 2 通りの表示がある（上の枠と下の `TODO` の木） |
| StableText | ステータス行を除き、スピナー文字と `36s`・`1m 5s`・`12:34`・`N tokens` を除く | 本文の中の時間表記も消えるが、画面ハッシュ用なので問題ない |

### claude

| 判定 | 目印 | 誤判定しやすい点 |
|------|------|------------------|
| asking | 画面の下 30 行以内に `❯` つきの選択肢（2 つ以上）があり、その 12 行以内に `ask_re` の案内文 | **入力欄の `❯`（上下を `─` の罫線が挟む）は選択肢にしない**。`❯ Try "…"` の idle を asking にしないための条件 |
| working | 下から 12 行以内の `esc to interrupt`（推定） | 実画面は idle・エラー後（`Cogitated for 0s · done`）しか採れていない。`done` 表示は作業中ではない |
| idle | 上下を罫線で挟んだ `❯` の入力欄 | 入力欄がない画面（起動の途中など）は unknown |
| Activity | working はスピナー行の `<記号> <動詞>…`（` (` より前）、asking は質問文（80 文字まで） | 質問文は選択肢の上の段落（`?` を含む段落を優先） |
| カード | `1.`・`2.` の番号つきで `❯` がカーソル。`Type something.`（または `Other…`）は Options に入れず AllowsCustom。`Chat about this` は選択肢に数えない。`/model` のメニューは文言のあとの説明・`✔` を除く | `(Recommended)`・`(recommended)` を Recommended にする（`Default (recommended)` もそうなる） |
| キー | カーソルが読めれば Up/Down の差分。読めないときだけ数字キー 1 つ（数字キーは押した時点で確定する画面があり、送ったあとに検証できないため Up/Down を優先） | 数字キーは 1〜9 のみ。10 番以降は `option_out_of_range` |
| Clean | 罫線・入力欄・ステータス行・作業中の行・TODO（`☐`/`☒`）を除く | |
| StableText | 作業中の行とステータス行を除き、経過時間・トークン数を除く | スピナー記号だけ違う 2 画面は同一になる |

### codex

| 判定 | 目印 | 誤判定しやすい点 |
|------|------|------------------|
| asking | 番号つき（`1.`）で `›` がカーソルの選択肢が 2 つ以上あり、近くに `ask_re` の案内文 | **番号のない `› Ask Codex to do anything`（入力欄）は選択肢にしない**（番号の有無で区別）。**2 番の文言が長いと折り返し、`(p)` だけの行が続く**（実画面）。選択肢の並びを上へたどるときに、この続き行をまたぐ（`FindChoice`）。またがないと、カーソルが 3 番のとき上の選択肢が見えず、承認カードを `unknown` にしてしまう（Issue #4 で直した） |
| working | 下から 12 行以内の `esc to interrupt`（`• Working (0s • esc to interrupt)`） | 本文（`• 指定されたコマンドを実行します。`）が出ている間も、作業中の行が下にあれば作業中。実行中のコマンドの行（`• Running …`）は作業中の行ではない |
| idle | 下から 12 行以内に `›` の入力欄（番号なし） | 実行後は `Worked for 6s • 19:54` の行が出るが、作業中ではない（`esc to interrupt` がない） |
| カード | 選択肢の末尾の `(y)`・`(p)`・`(esc)` を文言から外す。自由入力の項はないので AllowsCustom は常に false。**質問文は `Would you like to …?` の行**（`Reason: …？` や `$ command` の行ではない） | 質問文の下に `Environment: local`・`Reason: …`・`$ <コマンド>` が続く。`QuestionAbove`（`?` を含む最も近い段落）だと、`?`・`？` を含む `Reason` の行を質問と取り違えるため、`Would you like to`・`Do you want to` で始まる行を優先する（Issue #4 で直した） |
| キー | `PickKeys` はカーソルが読めれば Up/Down の差分、読めないときだけ数字キー 1 つ（Enter は含めない）。`CustomKeys` は常に `custom_not_allowed` | **実画面で数字キーは押した時点で確定する**（`3` を送ると「No」が実行され、`You canceled the request` になった）。Up/Down はカーソルが動くだけで、Enter で確定する。数字キーだと、送ったあとにカーソルの到達を確かめられず、続ける Enter が次の承認カードを承認しかねないので、Up/Down を優先する（Issue #4 で直した。以前は数字キーのみで、fakeagent は「カーソル移動＋Enter 確定」と仮定していた） |
| Clean | 起動の見出し行（`>_ OpenAI Codex (v…)`）・起動枠（`╭…╰`）・入力欄（画面の最後の `› `）・ステータス行（`← for agents · ? for shortcuts`・`N% context left`）・作業中の行を除く。履歴の `› <指示>` は残す | 下端のモデルの行（`<モデル名> default · <cwd>[ · <タイトル>]`）は除かない（形が一定でないため）。タイトルは作業のあとに付くので `StableText` が変わることがある |

## 共通の取り決め

- `Card.Cursor` は 1 始まり。「その他」の項は Options に入れず、カーソルがそこにあれば `Cursor = len(Options)+1`（pane の `ask_pick(text)` が到達の確認に使う）。
- `Detection.Activity`・`Elapsed` は画面に出ている文字列そのまま（加工しない）。
- `StableText` はスピナー・経過・トークン数・時刻を除くので、同じ内容でコマだけ違う 2 画面は同じ文字列になる。

## 実画面の採取手順

- omp: 専用ソケットの tmux（`-L <一意名> -f /dev/null new-session -d -x 120 -y 40`）で `omp` を起動し、`ask` ツールで質問カード・複数選択・「その他」入力中・回答後を採取。本番 tmux の稼働中ペインは `capture-pane -p` の読み取りだけ（`send-keys` なし）。サンプルの本文は無害な文に置き換え、ホスト名は `devbox` にした。
- claude: 同様に `claude`（v2.1.316）を起動。信頼確認（`❯ No, exit` / `Yes, I trust this folder`）、`/model` のメニュー、idle（`--permission-mode default` と bypass）、ログイン切れの表示を採取。**ログイン切れ（`Login expired · Please run /login`）でモデル呼び出しができず、作業中スピナー・ツール許可確認・質問カードは採れなかった**。
- codex（Issue #4）: `npm install --prefix <一時フォルダ> @openai/codex`（codex-cli 0.167.0）で入れ、専用ソケットの tmux（`-L <一意名> -f /dev/null new-session -d -x 120 -y 40`）で `codex -a on-request -s read-only` を起動して採取した（`--ask-for-approval` は `on-request`・`never` だけ。読み取り専用サンドボックスの外に出る `touch` で承認カードが出る）。idle・作業中（0〜2 秒）・承認カード（カーソル 1〜3）・数字キー `3` を押した直後・実行後の idle を採った。Up/Down でカーソルが動くことと、Enter・数字キーで確定することを確かめた。ファイル編集の承認カード・`/quit` は採っていない。本番の tmux・コードには触れていない（採取用の作業フォルダは `/tmp` の下）。
- 採取後は専用ソケットのサーバーを `kill-server` した。
