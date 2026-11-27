# API 仕様：ペインへテキストを送る（send_text）

[API 仕様](endpoints.md) の続き。§5 ツールのうち、§5.2 操作する（`/ops` のみ）の `send_text` を定める（[ペインを見る・操作する](endpoints-pane.md) から 300 行の上限のため分けた）。節番号は分割前のまま、§5.2 として引く。

改訂履歴は [API 仕様（本書）](endpoints.md#改訂履歴) にまとめてある。

## 5. ツール

### 5.2 操作する（`/ops` のみ）

共通の返り値：`{"ok": bool, "state": "...", ...}`。操作後の状態はスナップショットを取り直した結果。

#### `send_text`

| 引数 | 型 | 必須 | 説明 |
|------|-----|------|------|
| `pane` | string | 必須 | |
| `session` | string | 任意 | §4.1 |
| `text` | string | `file`・`task` がなければ必須 | 8000 字まで。許可する制御文字は `\n`・`\t` のみ。そのままの文を貼る（今までの使い方。組み立てない） |
| `file` | string | `text`・`task` がなければ必須 | サーバー上のファイル。`file_roots` の下、UTF-8 で 8000 字まで |
| `task` | string | `text`・`file` がなければ必須 | 作業中に足す追加の指示の本文。`dispatch` と同じ形に組み立てて貼る（下の「追加の指示の組み立て」）。`profile` が必須 |
| `profile` | string | `task` のとき必須 | `profiles.toml` のプロファイル名（`dispatch` と同じ。未知は `invalid_args`）。`task` なしで指定すると `invalid_args`（`details.field` は `task`） |
| `extra_rules` | string[] | 任意（`task` のときだけ） | 追加の条件（各 1 行。`dispatch` と同じ検証）。`task` なしで指定すると `invalid_args` |

`text`・`file`・`task` は 1 つだけ指定する（2 つ以上、またはどれも無ければ `invalid_args`）。

`file` は検証のあとに開き、開いた fd の実パスを Linux の `/proc/self/fd` から読んで、検証したパスと比べる（検証のあとに途中のディレクトリがリンクへ差し替えられても、外のファイルを読まない）。実パスが違えば `forbidden_path`（`details.field` は `file`・`reason` は `outside_roots`。何も送らない）。`/proc/self/fd` を読めないときは、確かめられないので `internal` で断る（Linux 以外や `/proc` の無いコンテナでは `file` を使えない。`text`・`task` は影響を受けない）。

```json
{"ok": true, "state": "working", "accepted": true, "chars": 412, "sent": "## 追加の指示（…）\n…", "reaction": "acted", "reaction_basis": "user_message"}
```

tmux の bracketed paste で貼り付け、1 秒待って Enter。貼り付けの確認メニュー（omp が設定 `paste.largeMenuThreshold`（既定 100）行以上を受けたときに開く。開いている間の Enter は依頼の送信にならずメニューの選択になる）が開いていれば、先に閉じるキー（omp は `Escape` = 「Paste inline」。本文は変わらない）を 1 回送り、閉じたのを画面で確かめてから Enter を送る。メニューを閉じる待ちと受理の待ちは 1 つの期限（1 秒のあと `accept_timeout`）を分け合う。画面が読めずメニューの有無が分からないとき、またはメニューが期限までに閉じなかったときは、Enter を送らず `{"ok": false, "accepted": false, "state": "unknown"}`（メニューが開いたままの画面は `unknown`。本文はメニューの中にあり、ツールからは復旧できないので、人が画面で閉じる）。メニューが開いていないときは `Escape` を送らない（作業中のエージェントを中断しないため）。入力欄が空になって `working` になったら `accepted: true`。`accept_timeout` 内に受理されなければ `{"ok": false, "accepted": false, "state": "idle"}`。Ctrl-C などの特別なキーは送れない。

Enter が効かないことがある（Issue #101。起動直後の omp は入力欄に本文を受けても送信を受け付けず、サーバーの負荷が高いと長引く）ため、Enter を送って 2 秒たっても画面が `idle` のまま（`working` になっていない = 送信されていない）なら、`accept_timeout` の期限の中で 2 秒おきに Enter を送り直す。画面が読めないとき（`unknown`）や `working`・`stalled` のままのときは送り直さない（作業中のエージェントに余計な Enter を送らない）。送り直した Enter が空の入力欄に届いても何も起きない。送り直しても期限までに受理されなければ、これまでどおり `{"ok": false, "accepted": false, "state": "idle"}`（`dispatch` も同じ）。決着の上限（1 秒 + `accept_timeout`）は変わらない。なお `dispatch` が `shell` のペインで omp を起動した直後（応答の `started: true`）は、起動した omp が画面に入力待ちを出したあとも初期化を続けて Enter を受け付けないことがあるため、受理を待つ上限を `accept_timeout` から `start_timeout`（既定 30 秒。`accept_timeout` のほうが長ければそちら）に延ばす（Issue #103。新しい設定は増やさない。起動済みの `idle` のペインへの `dispatch`・`send_text` は延ばさない）。この間も 2 秒おきの Enter の送り直しは続き、それでも受理されなければ今までどおり `ok: false`（決着の上限は 1 秒 + `start_timeout`）。Enter を送り直すたびに、ペイン・送り直した回数（`attempt`）・最初の Enter からの経過時間（`elapsed`）・読めた状態を `INFO` でログに残し、送り直したあとは結末（受理された `INFO`／受理を確かめられなかった `WARN`）と回数・経過時間を 1 行残す（Issue #103。依頼文・トークンは出さない。送り直さなかったときは何も出さない）。

##### 追加の指示の組み立て（`task`。Issue #120）

`task` を渡すと、`dispatch`（[§5.6](endpoints-report.md)）と同じ `render` で組み立てた全文を貼り、その全文を `sent` に返す（`text`・`file` のときは `sent` を付けない）。`command` は取らない。形は `dispatch` と見出し 1 行だけが違う。

```
## 追加の指示（いまの作業への追記。条件は dispatch で渡したものと同じ）   ← dispatch の「## やること」の代わり
<task>

## 条件
- <共通の条件>…／<プロファイルの条件>／<extra_rules の各行>   ← dispatch と同じ条件・同じ順・同じ `{report_path}` の置換
```

見出し以外の文は足さない。8000 字を超える・制御文字（`\n`・`\t` 以外）は `text_too_long`・`forbidden_control_char`（何も送らない）。`text`・`file` で渡す今までの使い方は残し、既定のまま変えない（`task` は選んで使う）。

##### 反応（`reaction`。Issue #120）

`working`・`stalled` のペインに貼ったときだけ、受理のあと、エージェントが指示をどう扱ったかを `reaction` と `reaction_basis` で返す（`idle` のペインへの送信には付けず、動きも変えない）。判断は画面の文字ではなく、エージェントの会話の記録（omp はセッションの jsonl。`/proc/<pid>/fd` で開いているものを選ぶ）だけで行うので、tern の画面でも普通の画面でも同じ結果になる。貼る前（Paste の直前）より後の行だけを見る。

| `reaction` | `reaction_basis` | 意味 |
|-----------|------------------|------|
| `acted` | `user_message` | 貼った文がユーザーの指示（`role: user`）として記録され、そのあとにモデルの応答がある。実行した |
| `ignored` | `tool_output` | ユーザーの指示にならず、ツールの出力（`toolResult`）やシステムの通知の中に現れた。作業中のツールの端末に入力として渡り、出力に混ざって戻ったもので、omp は外から紛れ込んだ文として扱う。無視した |
| `unverified` | `user_message_no_reply` | ユーザーの指示として記録されたが、上限までに応答がまだ無い |
| `unverified` | `not_found` | 上限までに記録のどこにも現れない（長いコマンドの実行中は、ツールの出力が記録されるのがコマンドの終了後なので、ここになりうる） |
| `unverified` | `record_unreadable`・`unsupported` | 記録を開けない・読めないエージェント（omp 以外）・pid が分からない。`unsupported` は待たずに返す |

待つ上限は、受理の確認が済んでから `accept_timeout` と `start_timeout` の長いほう（既定 30 秒。新しい設定は増やさない）。`acted`・`ignored`（応答が続いた）が決まれば、上限を待たずに返す（500 ms ごとに記録を読み直す）。決まらなければ最後の読み取りを返す。待つ間はペインの排他を外している（貼り付けと受理の確認までが排他の中。`send_escape` など同じペインへの操作を詰まらせない）。本文はログに出さず、結果（`reaction`・`basis`・待った時間）だけを INFO に残す。判断の理由は decisions.md P-109・P-110。

`ok`・`accepted` は Enter の受理（と、`acted` のとき記録上の受け取り）だけを指し、指示が実行されたかは `reaction` で見る。**`reaction: ignored` でも `ok: true` のままになりうる**（[endpoints.md §3](endpoints.md) の「期待した状態にならなければ `ok: false`」は、受理という期待した状態の話）。呼び出し側は `ok: true` だけで成功と判断せず、作業中のペインへ送ったときは `reaction` を見る。`acted` のときは、画面が受理を映していなくても（描き直しが止まった omp。Issue #113）受け取っているので `ok`・`accepted` を `true` にする。
