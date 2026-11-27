# アダプタの判定規則と P-22 の確定値

[アーキテクチャ設計 §9](decisions.md#9-提案した既定値と実装時確認事項) の P-22（状態判定・カードの正規表現の具体値）に対応する。
値は各アダプタの `defaults`（`adapter.go`）にある。`[agents.<name>]` で上書きできる（FR-AD-2）。
書かれたキーだけが置き換わる。`command`・`exit_command`・`auth_file` の空文字は設定エラー（起動拒否）で、ほかのキーの空文字・空配列は「上書きしない」。正規表現が不正なら Factory がエラーを返す。

| 確かさ | omp | claude | codex |
|--------|-----|--------|-------|
| 画面の出どころ | 実画面（omp v18.14.1、120×40） | **すべて実画面**。idle・信頼確認・`/model` メニューは v2.1.316、作業中・許可確認（Bash）・質問（AskUserQuestion）は v2.1.332（120×40、`--model=haiku --effort=low`。Issue #154） | **すべて実画面**。idle・作業中・コマンド実行の承認カード（`touch`）・承認後の idle は codex-cli 0.167.0（120×40。Issue #4）、ファイル編集の承認カード（`patch-1`〜`3`）・`/quit`・`--model=` は codex-cli 0.172.0（120×40。Issue #155）。0.172.0 の idle・作業中・編集後の idle も採り（`*-v0172.txt`）、0.167.0 と同じ規則で読めることをテストで確かめた |
| サンプル | `testdata/screens/omp/` | `testdata/screens/claude/`（すべて実画面） | `testdata/screens/codex/`（すべて実画面） |

## 既定値（Patterns）

| キー | omp（実画面で確定） | claude | codex（実画面で確定） |
|------|--------------------|--------|---------------|
| `process_match` | `(^|/)omp$`（実機の argv[0] は `omp`） | `(^|/)claude$`（実機の argv[0] は `claude`。実行ファイルは `~/.local/share/claude/versions/<ver>` へのリンクで、cmdline は `claude --permission-mode default`） | `(^|/)codex$` |
| `working_re` | `^\x{e0b6}\s*[\x{2800}-\x{28ff}]`（ステータス行の左端の直後に点字スピナー） | `esc to interrupt`（実画面：ステータス行 `⏸ manual mode on · esc to interrupt · ← 1 agent`。作業中はずっと出る。スピナー行 `* Zesting… (1s · ↓ 36 tokens · thinking with low effort)` は考え中だけで、本文を出している間は無く、行に `esc to interrupt` も無い） | `esc to interrupt`（実画面の `• Working (0s • esc to interrupt)`。0.167.0・0.172.0） |
| `ask_re` | `^╭─ (?:Ask\|Custom answer)\b` | `(?i)\b(?:Esc to cancel\|Enter to (?:select\|confirm\|set)\|Do you want to\|Would you like to)\b`。実画面の案内文は、信頼確認・`/model` が `Enter to confirm`・`Enter to set as default`、許可確認（Bash）が `Esc to cancel · Tab to amend`（カーソルが 1 のときだけ。2 以降は `Esc to cancel`）と質問文 `Do you want to proceed?`、質問（AskUserQuestion）が `Enter to select · ↑/↓ to navigate · Esc to cancel` | `(?i)(?:Press enter to confirm or esc to cancel\|Would you like to\|Do you want to)`（実画面の `Would you like to run the following command?`・`Press enter to confirm or esc to cancel`） |
| `idle_re` | `^\x{e0b6}`（ステータス行があり、作業中でも質問でもない） | `^\s*[❯>](?:[\s\x{a0}]\|$)`（入力欄。上下を罫線 `─` が挟む。実画面） | `^\s*›(?:[\s\x{a0}]\|$)`（入力欄 `› Ask Codex to do anything`。実画面） |
| `status_line_re` | `^\x{e0b6}`（ステータス行） | `^\s*(?:[⏵⏸]\|\? for shortcuts\|esc to interrupt\b\|Not logged in\b)`（実画面：`⏸ manual mode on · …`・`⏵⏵ bypass permissions on …`・`⏵⏵ auto mode on …`・`⏸ plan mode on …`・`? for shortcuts`。`Not logged in` は推定のまま） | `^\s*(?:\d+% context left\|(?:←[^·]*·\s*)?\? for shortcuts\|Ctrl\+C again to quit)`（実画面の下端は `←  for agents · ? for shortcuts`。**`N% context left` は実画面に出なかった**ので推定のまま残す） |
| `border_re` | `^\s*[╭├╰]─` | `^\s*(?:[─━▔]{3,}\|[╭├╰]─)` | `^\s*[╭╰]─` |
| `spinner_chars` | `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`（実画面） | `✻✢✳✶✽✦·*`（実画面で確認したのは `✻`・`✽`・`·`・`*`。`✢✳✶✦` は採れておらず推定のまま。`✳` はペインのタイトルには出る） | `•◦`（実画面の `• Working`・`• Running`・`◦ Working`。0.172.0 は `•` と `◦` が交互に出る） |
| 起動・終了 | `omp`・`/exit`（実機で `/exit` + Enter により `Closing session…` が出てプロセスが終わり、ペインがシェルに戻るのを確認。omp v18.15.0） | `claude`・`/exit`（実機で `/exit` + Enter によりプロセスが終わるのを確認） | `codex`・`/quit`（実機で `/quit` + Enter によりプロセスが終わり、ペインがシェルに戻るのを確認。`pane_close` も通る。codex-cli 0.172.0。起動は `--model=<値>` を足せる） |

omp のステータス行は Nerd Font の私用領域の文字（`U+E0B6` 左端、`U+E0B1` 区切り、`U+F054` カーソル、`U+F10C` など）を使う。
`capture-pane -p` にはこれらがそのまま出る。ステータス行は `U+E0B6 <スピナー> <経過> U+E0B1 …`
の形で、スピナーと経過（`36s`・`2m`・`4h`）は作業中のときだけ入る。

## 判定の規則（どの目印で何を判定するか）

共通の順序は「質問カード → 作業中 → 入力待ち → どれでもなければ `unknown`」。`shell`・`stalled` は `pane` が決める。

### omp

| 判定 | 目印 | 誤判定しやすい点 |
|------|------|------------------|
| asking | 画面で**最後の枠**（行頭が `╭` の最後の行）の上枠が `╭─ Ask`（選択。複数の質問のフォームは `╭─ Ask · 1 of 2 answered`）または `╭─ Custom answer: …`（自由入力中。v18）で、その下枠（行頭が `╰──`）が画面の最後から 10 行以内にあること。**枠のあとに続く行は問わない**（v18 は枠が画面の最後の行。v19 は活動行・タイトル・ステータス行・入力欄の下枠 `╰─` が続く。Issue #111） | 回答後に残る結果の枠は `╭─── Ask …`（`─` が 3 つ以上＋記号）で目印に当たらない（そのあとの枠も最後の枠になる）。**v19 でも「画面の最後の行が `╰──`」だけを条件にすると、枠の下にステータス行が出るので asking にならず idle と読む（2026-11-19、B1。webhook も飛ばなかった）**。`testdata/screens/omp/ask-v19-*.txt`（実画面）・`ask-v19-b1.txt`（B1 の質問を実画面の形に組み直したもの） |
| working | ステータス行（下から 8 行以内の `U+E0B6` 行）のスピナー位置に点字文字 | 本文に `⠋` などが出ても、ステータス行の位置を固定しているので影響しない |
| idle | ステータス行があり、上の 2 つに当たらない | 起動直後（ロゴの画面）も idle。スピナーも経過もない。負荷が高いと画面の描き直しが止まり、作業中でも起動画面のまま idle と読める（Enter の受理を確かめられなかったペインは、`~/.omp/logs/omp.<日付>.<pid>.log` の作業の行で working と読む。FR-STATE-12・P-105。件を送ったあとも見続ける間隔と上限は P-107。長いコマンドの間は、モデルの呼び出しの行が出ないので、omp が開いているセッションの記録 `~/.omp/agent/sessions/<作業ディレクトリ>/<日時>_<id>.jsonl` の最後の `message` の行が `user`・`toolResult`・`assistant`＋`stopReason: toolUse` のとき（番の途中）も working と読む。`stop`・`aborted`・`error` は番の終わり。P-108） |
| Activity | ステータス行の直上の行（`<記号> Working…`・`<記号> Asking fruit choice`）の文言。右寄せのタイトル（`U+F1042 FRUIT: …`）は字下げが大きいので除く。asking は、枠のあとにステータス行があればその直上（v19）、なければ枠の上枠の直上（v18） | 活動行がないとき（`You chose …` が直上に来るなど）は空 |
| Elapsed | ステータス行の最初の区切り `U+E0B1` より左の `\d+(s\|m\|h)` | 時間が長いと単位が粗くなる（`2s`→`4m`→`4h`）。分・秒の混在はない |
| カード | 見出し行＝質問文。選択肢は `│ [U+F054] <記号> ラベル │`（長い選択肢は次の行に折り返す。日本語どうし・ASCII どうし以外の境目は空白を入れずにつなぐ）。`(Recommended)` は Recommended=true で文言から外す。自由入力の行は Options に入れず AllowsCustom=true（カーソルがそこなら Cursor=len(Options)+1）。行は v18 が `Other (type your own)`、v19 が `Other: type your own`（カーソルがあると `Other:  type your own`、入力したあとは `Other: <入力した文>`）。**v18 の枠は `├` で見出し・選択肢・フッターに区切られ、選択肢は 2 つ目の区画。v19 は区切りもフッターもなく、見出し → 選択肢 → 空行 → `Submit` の行の順**（カーソルが `Submit` にあると `❯ Submit`。選択肢の行ではない）。質問が複数のフォームは、**開いている質問（選択肢の行がある質問）だけ**を読む。選び済みの質問は `✓ 見出し` と `✓ 選んだ文言` の 2 行にたたまれ、まだの質問は `○ 見出し` と横並びの選択肢で、どちらも選択肢の行の形ではない | 複数選択（チェックボックス `U+F096`（未チェック）/`U+F14A`（チェック済み）。v18 は見出しの 1 行目に `Submit` のタブ）も同じ形。単一選択の丸は `U+F10C`（未選択）・`U+F192`（選択中。最初は先頭の選択肢）。**カーソルが `Submit` の行にある間は、質問が 1 行にたたまれて選択肢の行が見えない**ので ParseCard は ok=false（asking のまま。回答の確定は Enter 1 回）。`Custom answer` の枠は選ぶ対象がないので ParseCard は ok=false。Esc を 1 回押すと入力欄へフォーカスが移り、カーソルの印がなくなる（Cursor=0）。もう 1 回でキャンセル（`Ask input was cancelled`。idle） |
| キー | `PickKeys` は Up/Down の差分だけ、`CustomKeys` は「その他」まで Down。Enter は含めない。カーソルが読めないときは `cursor_mismatch`。キーを送った直後の画面は描き直し前のことがあるので、呼び出し側（`ask_pick`）が反映を待ってから検証する。**v19 は選択肢を選ぶ Enter のあとカーソルが `Submit` の行へ移り、もう 1 回 Enter で確定する**（`agent.Submitter.SubmitFocused` が `❯ Submit` を読む。`ask_pick` が Enter を 1 回だけ足す）。**複数選択は Space でチェックを付け外しする**（`agent.Toggler.ToggleKeys`。カーソルの行が未チェックのチェックボックスのときだけ Space を返し、`ask_pick` が選ぶ前に送って、チェックが映るのを確かめる）。実機（v19.0.0）の Enter は、チェックの付いた質問を確定して次へ進む | 数字キーは使わない（v18 のフッターは `↑/↓ move`・`select` のみ。v19 はフッターなし） |
| 貼り付けの確認メニュー | 画面の最後の枠が `╭─ Pasted N lines ─…`（`N` は貼った行数）。設定 `paste.largeMenuThreshold`（既定 100）行以上を貼ると開く（omp v18.20.3 の実機。100 行未満は入力欄に `[Paste #N, +M lines]` の印で載るだけ）。選択肢は「Attach as a wrapped block」（初期位置）・「Attach as local file」・「Paste inline」。**開いている間の Enter は初期位置の「wrapped block」を選ぶだけで依頼は送信されない**（Issue #99）。`Escape` は「Paste inline」（フッター `⎋ to paste inline`）で、本文は変わらず入力欄に印として載る。メニューが開いている間はステータス行が見えないので `Detect` は `unknown` | Nefos Hub は `Escape` で閉じてから Enter を送る。画面にメニューがないときは `Escape` を送らない（作業中の中断になる）。`testdata/screens/omp/paste-prompt.txt`・`paste-pending.txt` |
| 自由入力 | v18：「その他」で Enter → `╭─ Custom answer: <質問>` の枠（`> ` の行へ入力）。Enter で送信（フッターには `Ctrl+Q submit` もある）。v19：別の枠は開かず、`Other:` の行にカーソルがあるときの入力・貼り付けがそのまま行に入り（複数行の貼り付けは 1 行にまとまる）、選択済みになる。Enter で確定して `Submit` の行へ移る | 送信後は `Ask` の結果枠にチェック付きで本文が出て、そのまま作業中に戻る |
| Clean | 画面下のステータス行・活動行・タイトル行、`╭╰├` の枠線、`Todo N tasks` の枠、`TODO` の木、枠の左右の `│` を除く | TODO は 2 通りの表示がある（上の枠と下の `TODO` の木） |
| StableText | ステータス行を除き、スピナー文字と `36s`・`1m 5s`・`12:34`・`N tokens` を除く | 本文の中の時間表記も消えるが、画面ハッシュ用なので問題ない |

### omp（tern の画面。TSP）

omp は、tern が管理する tmux のセッションでは tern の画面（TSP）を出す（条件は [P-111](decisions.md)）。`capture-pane` は空で、`pane_title` は `tsp;f;{"sf":"s:1","s":<連番>,"ops":[…]}`（最後の 1 フレームの差分）になる。アダプタは `agent.Native` も実装し、`pane` はタイトルが `tsp;` で始まる omp のペインだけ、`capture-pane` の代わりに**会話の記録（`/proc/<pid>/fd` が指す `~/.omp/agent/sessions/<cwd>/<日時>_<id>.jsonl` の末尾）から組み立てた画面**を `Detect` などに渡す（[P-112](decisions.md)。限界は P-113）。

| 項目 | 組み立て方 | 普通の画面との対応 |
|------|-----------|-------------------|
| 本文 | 直近のやり取り（新しいほうから最大 40）。指示・応答は 1 行ずつ字下げ 1、ツールの呼び出しは ` ▸ <名前>: <意図>`。ツールの結果は出さない | `pane_tail` の中身（`Clean` を通る） |
| working | 記録の最後が番の途中（指示・ツールの結果・`stopReason=toolUse` の応答）。活動行 `U+F12B7 <最後のツールの意図、無ければ Working…>` とステータス行 `U+E0B6 ⠋ <経過> U+E0B1 tern`（経過は番の始まり＝最後の指示から。`36s`・`17m05s`・`1h05m`） | `working_re`（スピナー）・`Activity`・`Elapsed` |
| asking | 最後のやり取りが `ask` の呼び出し（結果がまだ無い）。引数の `html`（`<fieldset><legend>`＋radio・checkbox・select）か `questions` の配列から、1 問目を `╭─ Ask …╮`・見出し・選択肢（`Other: type your own`・`Submit`）に組み立てる。カーソルは `recommended`（html は `checked`）の選択肢、無ければ 1 番。`recommended` の選択肢には ` (recommended)` を足す | `ask_re`・`ParseCard`・`PickKeys`・`Toggler`・`SubmitFocused` |
| idle | 上のどれでもない（記録が無い・開いていない起動直後も） | `idle_re`（`U+E0B6` 行） |
| StableText | ステータス行（経過・スピナー）を除く。経過時間の更新では変わらず、会話が進むと変わる | 画面ハッシュ・`changed_at`・`stalled` |
| `title`（`panes_status`） | `""`（フレームの JSON を出さない） | 普通の画面は `pane_title` のまま |

`NativeDelivered(pid, text, since)` は、`since` 以降のユーザーの指示に `text` の最後の非空行の末尾 30 字が含まれるか。`send_text`・`dispatch` が、すでに作業中のペインで受理を確かめるのに使う（入力欄が見えないため）。記録を開いていない pid は `false`、組み立てを読めない（記録を開けない）ときは `capture-pane` に戻る。実機の記録は `testdata/native/omp/`。

### claude

| 判定 | 目印 | 誤判定しやすい点 |
|------|------|------------------|
| asking | 画面の下 30 行以内に `❯` つきの選択肢（2 つ以上）があり、その 12 行以内に `ask_re` の案内文 | **入力欄の `❯`（上下を `─` の罫線が挟む）は選択肢にしない**。`❯ Try "…"` の idle を asking にしないための条件 |
| working | 下から 12 行以内の `esc to interrupt`（ステータス行。v2.1.332 の実画面） | `✻ Cooked for 3s · done 10:22 PM`（終わった印）は作業中ではない。許可確認・質問のカードが出ている間はステータス行に `esc to interrupt` が出ない（asking が先に当たる） |
| idle | 上下を罫線で挟んだ `❯` の入力欄 | 入力欄がない画面（起動の途中など）は unknown |
| Activity | working はスピナー行（`<記号> <動詞>…` で始まる行。` (` より前）。経過はその括弧の `(1s · …)`。スピナー行は考え中だけで、本文を出している間は無いので、Activity・Elapsed は空（`pane` の経過で補う）。asking は質問文（80 文字まで） | 質問文は選択肢の上の段落（`?` を含む段落を優先。許可確認は `╌╌╌` の区切りで段落を切り、`Do you want to proceed?` だけを質問文にする） |
| カード | `1.`・`2.` の番号つきで `❯` がカーソル。`Type something.`（または `Other…`）は Options に入れず AllowsCustom。`Chat about this` は選択肢に数えない。`/model` のメニューは文言のあとの説明・`✔` を除く。質問の選択肢は説明の行（`りんご` など）が付く。許可確認（Bash）は 4 択（`Yes`・`Yes, and always allow access to <dir> from this project`・`Yes, and switch to auto mode · …`・`No`。手動モードの場合。選択肢はモードや対象で変わる） | `(Recommended)`・`(recommended)` を Recommended にする（`Default (recommended)` もそうなる） |
| キー | カーソルが読めれば Up/Down の差分。読めないときだけ数字キー 1 つ（**実画面で、質問カードは数字キー `2` を押しただけで確定した**。送ったあとに検証できないため Up/Down を優先）。`ask_pick` で許可確認・質問に答えられた（v2.1.332） | 数字キーは 1〜9 のみ。10 番以降は `option_out_of_range` |
| Clean | 罫線・入力欄・ステータス行・作業中の行（スピナー行を含む）・TODO（`☐`/`☒`）を除く | |
| StableText | 作業中の行（スピナー行を含む）とステータス行を除き、経過時間・トークン数を除く | スピナー記号だけ違う 2 画面は同一になる |

### codex

| 判定 | 目印 | 誤判定しやすい点 |
|------|------|------------------|
| asking | 番号つき（`1.`）で `›` がカーソルの選択肢が 2 つ以上あり、近くに `ask_re` の案内文 | **番号のない `› Ask Codex to do anything`（入力欄）は選択肢にしない**（番号の有無で区別）。**2 番の文言が長いと折り返し、`(p)` だけの行が続く**（実画面）。選択肢の並びを上へたどるときに、この続き行をまたぐ（`FindChoice`）。またがないと、カーソルが 3 番のとき上の選択肢が見えず、承認カードを `unknown` にしてしまう（Issue #4 で直した） |
| working | 下から 12 行以内の `esc to interrupt`（`• Working (0s • esc to interrupt)`。0.172.0 は `◦ Working` も出る） | 本文（`• 指定されたコマンドを実行します。`）が出ている間も、作業中の行が下にあれば作業中。実行中のコマンドの行（`• Running …`）は作業中の行ではない |
| idle | 下から 12 行以内に `›` の入力欄（番号なし） | 実行後は `Worked for 6s • 19:54` の行が出るが、作業中ではない（`esc to interrupt` がない） |
| カード | 選択肢の末尾の `(y)`・`(a)`・`(p)`・`(esc)` を文言から外す（ファイル編集の承認カードは 3 択：`Yes, proceed`・`Yes, and don't ask again for these files`・`No, and tell Codex what to do differently`。0.172.0）。自由入力の項はないので AllowsCustom は常に false。**質問文は `Would you like to …?` の行**（`Reason: …？` や `$ command` の行ではない） | 質問文の下に `Environment: local`・`Reason: …`・`$ <コマンド>` が続く。`QuestionAbove`（`?` を含む最も近い段落）だと、`?`・`？` を含む `Reason` の行を質問と取り違えるため、`Would you like to`・`Do you want to` で始まる行を優先する（Issue #4 で直した） |
| キー | `PickKeys` はカーソルが読めれば Up/Down の差分、読めないときだけ数字キー 1 つ（Enter は含めない）。`CustomKeys` は常に `custom_not_allowed` | **実画面で数字キーは押した時点で確定する**（`3` を送ると「No」が実行され、`You canceled the request` になった）。Up/Down はカーソルが動くだけで、Enter で確定する。数字キーだと、送ったあとにカーソルの到達を確かめられず、続ける Enter が次の承認カードを承認しかねないので、Up/Down を優先する（Issue #4 で直した。以前は数字キーのみで、fakeagent は「カーソル移動＋Enter 確定」と仮定していた） |
| Clean | 起動の見出し行（`>_ OpenAI Codex (v…)`）・起動枠（`╭…╰`）・入力欄（画面の最後の `› `）・ステータス行（`← for agents · ? for shortcuts`・`N% context left`）・作業中の行を除く。履歴の `› <指示>` は残す | 下端のモデルの行（`<モデル名> default · <cwd>[ · <タイトル>]`）は除かない（形が一定でないため）。タイトルは作業のあとに付くので `StableText` が変わることがある |

## 共通の取り決め

- `Card.Cursor` は 1 始まり。「その他」の項は Options に入れず、カーソルがそこにあれば `Cursor = len(Options)+1`（pane の `ask_pick(text)` が到達の確認に使う）。
- `Detection.Activity`・`Elapsed` は画面に出ている文字列そのまま（加工しない）。
- `StableText` はスピナー・経過・トークン数・時刻を除くので、同じ内容でコマだけ違う 2 画面は同じ文字列になる。

## 実画面の採取手順

- omp: 専用ソケットの tmux（`-L <一意名> -f /dev/null new-session -d -x 120 -y 40`）で `omp` を起動し、`ask` ツールで質問カード・複数選択・「その他」入力中・回答後を採取。本番 tmux の稼働中ペインは `capture-pane -p` の読み取りだけ（`send-keys` なし）。サンプルの本文は無害な文に置き換え、ホスト名は `devbox` にした。
- claude: 同様に `claude`（v2.1.316）を起動。信頼確認（`❯ No, exit` / `Yes, I trust this folder`）、`/model` のメニュー、idle（`--permission-mode default` と bypass）、ログイン切れの表示を採取。このときは**ログイン切れ（`Login expired · Please run /login`）でモデル呼び出しができず、作業中スピナー・ツール許可確認・質問カードは採れなかった**。
- claude（Issue #154）: ログインした状態の v2.1.332 を、専用ソケットの tmux（`-L <一意名>`、120×40）に `pane_start`（`model=haiku`・`thinking=low`）で起動して採取した。許可確認は **Shift+Tab で手動モード（`manual mode on`）にしてから** `Use the Bash tool to run exactly: touch note.txt` を頼む（`bypass permissions` や `auto mode` のままだと出ない）。カーソル 1〜4 を `Down` で動かして採り、質問は `AskUserQuestion` を使う指示で出す（カーソル 1〜4）。作業中は短い一覧の生成（`List the integers 1 to 150…`）を頼み、0.4 秒ごとに `capture-pane` した画面から、スピナー行ありと本文を出している間を選んだ。単独の `sleep` はハーネスが拒むので長いコマンドは使えない。費用は haiku・effort low の短い指示だけ。
- codex（Issue #4）: `npm install --prefix <一時フォルダ> @openai/codex`（codex-cli 0.167.0）で入れ、専用ソケットの tmux（`-L <一意名> -f /dev/null new-session -d -x 120 -y 40`）で `codex -a on-request -s read-only` を起動して採取した（`--ask-for-approval` は `on-request`・`never` だけ。読み取り専用サンドボックスの外に出る `touch` で承認カードが出る）。idle・作業中（0〜2 秒）・承認カード（カーソル 1〜3）・数字キー `3` を押した直後・実行後の idle を採った。Up/Down でカーソルが動くことと、Enter・数字キーで確定することを確かめた。ファイル編集の承認カード・`/quit` はこのときは採らなかった。本番の tmux・コードには触れていない（採取用の作業フォルダは `/tmp` の下）。
- codex（Issue #155）: 同じく専用ソケットの tmux に、`[agents.codex] command = "codex -a on-request -s read-only"` を置いた検証用の `swarm-console serve`（`127.0.0.1` の別ポート）から `pane_start`（`model=gpt-5.6-luna`）で起動した（codex-cli 0.172.0）。`Create a file hello.txt … Use apply_patch` でファイル編集の承認カード（カーソル 1〜3）を採り、`ask_pick` で承認し、`pane_close`（`/quit`）で終了を確かめた。
- 採取後は専用ソケットのサーバーを `kill-server` した。
