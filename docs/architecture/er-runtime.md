# ER / データモデル：tokens.toml・実行時のペイン状態

[ER / データモデル](er.md) の続き。§3 tokens.toml と §4 実行時のペイン状態（メモリ）を定める。

改訂履歴は [ER / データモデル（本書）](er.md#改訂履歴) にまとめてある。

## 3. tokens.toml

```mermaid
erDiagram
    TOKEN {
        string name PK "^[a-z0-9][a-z0-9_-]{0,31}$ 一意"
        string scope "report | ops"
        string hash "sha256:<hex 64>"
        string created_at "RFC 3339"
        array allowed_sessions "任意。触れる tmux セッション。省略は config の sessions 全部"
        string revoked_at "RFC 3339、無効のときだけ"
    }
```

```toml
[[token]]
name = "ops-client"
scope = "ops"
hash = "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
created_at = "2026-10-31T22:00:00+09:00"
allowed_sessions = ["0"]

[[token]]
name = "agent-b"
scope = "ops"
hash = "sha256:..."
created_at = "2026-11-02T18:00:00+09:00"
allowed_sessions = ["1"]

[[token]]
name = "routine-report"
scope = "report"
hash = "sha256:..."
created_at = "2026-10-31T22:05:00+09:00"
# allowed_sessions を書かない = config.toml の sessions 全部
revoked_at = "2026-11-02T09:00:00+09:00"
```

| 制約 | 内容 |
|------|------|
| 一意性 | `name` は一意。無効にした名前も再利用しない（ログの追跡性） |
| 平文 | 保存しない。`token add`（と `pair`）が `sc_` ＋ base64url(32 バイト) を生成し、標準出力に 1 度だけ出す（`pair` は接続文字列の中に入れる）。`token list` はハッシュも平文も出さない |
| 失効 | `token revoke <名前>` が `revoked_at` を付ける。失効済みは一致しても 401 |
| 照合 | 受け取ったトークンの SHA-256 を、全エントリの `hash` と定数時間比較（早期終了なし）。失効済みは一致扱いにしない |
| 書き込み | CLI だけが書く。一時ファイル → rename、権限 600 |
| 許可セッション | `allowed_sessions`（string 配列、任意）。キーが無ければ `config.toml` の `sessions` 全部。あるときは 1 要素以上（空配列は読み込みで拒否）・各要素は非空で `.`・`:`・制御文字を含まない・重複なし（`CheckSessions` と同じ文字規則）。実効の許可セッションは `allowed_sessions` と `sessions` の共通部分で、ツールの呼び出しごとに求める（`auth.Client.AllowedSessions`：`nil` は「`sessions` 全部」、値があれば明示のリスト）。`token add --sessions 0,1` が書く（省略ならキーを書かない）。変えるには `token revoke` してから `token add --sessions` で発行し直す（FR-SESS-1〜3・P-76〜P-78） |
| 起動検証 | 権限が 600 でない・TOML が壊れている・未知の `scope`・不正な `allowed_sessions`（空配列・重複・不正な文字）は起動拒否。`serve` の起動時は、さらに `allowed_sessions` に `config.toml` の `sessions` にない名前があれば起動拒否（エラーにトークン名とセッション名を出し、ハッシュは出さない）。起動後の再読み込みで食い違ったときは警告ログだけを出し、実効の許可セッションは共通部分にする（P-76） |

## 4. 実行時のペイン状態（メモリ）

```mermaid
erDiagram
    SNAPSHOT ||--o{ PANE_STATE : "panes"
    SNAPSHOT ||--|| HOST_STATE : "host"
    SNAPSHOT ||--o{ WARNING : "warnings"
    PANE_STATE ||--o| CARD : "asking のとき"
    PANE_STATE ||--o| PANE_NOTE : "note_summary"

    SNAPSHOT {
        string at "取得時刻（report の時刻）"
        object layout "段数・列数"
        object usage "最後の使用状況の結果（収集器から。status・アカウント。アカウントごとに name・reason（without_usage | no_email | 空）・枠）"
        object omp "omp の更新確認の結果（収集器から）"
        object update "swarm-console の更新確認の結果（host_status.update と同じ）"
        object auth "エージェントのログイン期限（スナップショットを作るときに読む）"
    }
    PANE_STATE {
        string id PK "tmux ID %12"
        string pos "位置 T1 / work:B2 / w1.T2"
        string session
        int window
        int pane_index
        int top
        int left
        string agent "omp | claude | codex | null"
        string state "working | asking | idle | shell | stalled | unknown"
        string title
        string cwd
        string elapsed "例 42m（取れなければ null）"
        string activity "いま何をしているかの 1 行"
        string changed_ago "最後に画面が変わってから"
        string screen_hash "揮発部分を除いた本文のハッシュ"
        string changed_at "ハッシュが変わった時刻"
        bool report_exists "最終報告ファイルあり"
    }
    CARD {
        string question
        array options "n・label・recommended"
        int cursor "現在のカーソル番号"
        bool allows_custom "「その他」を許すか"
    }
    HOST_STATE {
        array load "1・5・15 分"
        int cpus
        float mem_total_gb "MemTotal"
        float mem_free_gb "MemAvailable"
        object mem "total_gb・used_gb・used_percent（保持はせず、描画のとき mem_total_gb と mem_free_gb から作る。MemTotal が 0 以下・MemAvailable が MemTotal より大きいときは取得できず。P-93）"
        array disk "mount・free_gb・available・total_gb・used_percent"
        object cpu "used_percent（前のサンプラーとの差。最初は取得できず）"
        object swap "total_gb・used_gb・used_percent"
    }
    WARNING {
        string code "layout_changed | tmux_unavailable | session_missing | pane_unreadable"
        string message
    }
```

| 項目 | 規則 |
|------|------|
| 不変性 | スナップショットは作ったあと変更しない。次のスナップショットが作られるまで読み取り専用 |
| `state` の決定 | 質問カード → 作業中 → エージェントあり＝`idle` → エージェントなし＋シェル＝`shell` → それ以外 `unknown`。`working` で `now - changed_at ≥ stalled_after` なら `stalled` |
| `changed_at` | 前回と `screen_hash` が違えば更新。起動直後は起動時刻 |
| `pos` | FR-POS のとおり。固定名があればそれ。スナップショットの `pos` は全体の位置（既定セッションは `T1`、それ以外は `1:T1`）で、最終報告ファイル名と複数セッション許可の resources の名前はこれから付く。1 つのセッションに絞った応答は、そのセッションのペインから接頭辞なしのセッション内の位置を付け直して返す（FR-SESS-7） |
| セッションでの絞り込み | スナップショットは全セッション分を持つ。`session` を取るツールは、決まったセッションのペインだけを取り出し、`layout`・`warnings` もそのセッションの分に絞って返す（FR-SESS-7・8）。並びの変化（`layout_changed`）の検知もセッションごと |
| `elapsed` | アダプタが画面の経過時間表示を読めたらそれ。読めなければ、サーバーが `working` を観測し始めた時刻からの経過。それも不明なら `null` |
| 持たないもの | 画面の全文は保持しない（`pane_tail`・`ask_card` は呼ばれたとき取り直す）。ハッシュだけ残す |
