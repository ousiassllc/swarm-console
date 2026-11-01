# 認証期限の読み先（P-16）

[アーキテクチャ設計 §9](decisions.md#9-提案した既定値と実装時確認事項) の P-16 の確定値。

読むのは **期限だけ**。トークン文字列・メールなどは返り値・ログ・エラーに出さない。ファイルが無い・壊れている・形式違いは `unknown`（エラーにしない）。`ExpiresAt` が 48 時間以内なら `expiring_soon`、過ぎていれば `expired`、それ以外 `valid`。

| agent | 読み先 | 読むキー | 判断 |
|-------|--------|----------|------|
| claude | `~/.claude/.credentials.json` | `claudeAiOauth.expiresAt`（epoch ミリ秒）・`claudeAiOauth.refreshTokenExpiresAt`（同） | **`expiresAt` が文字どおり 0 なら、`refreshTokenExpiresAt` が未来でも `expired`・期限なし**。それ以外は有効な値（> 0）のうち遅いほうを期限とする（アクセストークンは数時間で切れ、更新トークンで自動更新されるため、「ログインが生きている」期限は更新トークン側）。有効な値が 1 つも無ければ `expired`・期限なし。`claudeAiOauth` が無い・数値でないなら `unknown`。判定表は下 |
| codex | `~/.codex/auth.json` | `tokens.access_token`（JWT）の payload の `exp`（epoch 秒）だけ | `exp` を期限とする。JWT でない・`exp` が無いなら `unknown`。`last_refresh` は読まない（期限は `exp` が正） |
| omp | 設定 `[agents.omp] auth_file` | 最上位の `expires_at`・`expiresAt`・`exp` のうち最初に見つかったもの | 数値は `1e12` 以上ならミリ秒、未満なら秒。文字列は RFC 3339。0 以下・無し・形式違いは `unknown`。`auth_file` が未設定なら `unknown` |

どのエージェントも `auth_file` を設定すればそちらを（`~/` 展開して）読む。形式はそのエージェントの上の行と同じ。

## Claude の判定表と `expiresAt: 0` を `expired` とする理由（P-16）

`expiresAt` が文字どおり **0** のときは、`refreshTokenExpiresAt` が未来でも `expired`（期限なし）。0 は正常なトークンでは書かれない値で、ログアウト状態や書き込みの不具合を示す。Claude Code 自身も `expiresAt: 0` では「Login expired」になる。`0` は「無期限」ではない。

`expiresAt` が 0 より大きい過去の値で、`refreshTokenExpiresAt` が未来なら、通常の自動更新（アクセストークンは数時間で切れ、更新トークンで更新される）として `valid`。

判定表（`claudeAiOauth` がある場合。「有効な値」は `> 0` の数値。0 以外の行の期限は有効な値のうち遅いほう）：

| # | `expiresAt` | `refreshTokenExpiresAt` | 判定 | 備考 |
|---|-------------|------------------------|------|------|
| 1 | **0** | 未来 | **`expired`**（期限なし） | `expiresAt: 0` は失効の値。更新トークンが未来でも拾わない |
| 2 | **0** | 過去・0・null・欠落 | `expired`（期限なし） | この機の実ファイルもここ（`expiresAt: 0`・更新は過去・トークン文字列は空） |
| 3 | 過去（> 0） | 未来 | 更新トークンの期限で `valid`／`expiring_soon` | 通常の自動更新 |
| 4 | 過去（> 0） | 過去 | `expired` | 期限は遅いほう |
| 5 | 未来 | 未来・過去・欠落 | 遅いほうの期限で判定 | |
| 6 | null・欠落 | 未来 | 更新トークンの期限で判定 | |
| 7 | null・欠落 | 0・null・欠落 | `expired`（期限なし） | 有効な値が 1 つも無い（ログアウト） |
| 8 | 文字列など数値でない | — | `unknown` | JSON 型違い |

- トークン文字列（`accessToken`・`refreshToken`）は読まない（秘密を扱わない）。空かどうかでは判定せず、期限の数値だけで決める。そのため、文字列が空でも期限が未来なら `valid`（行 3・5・6）。
- `claudeAiOauth` が無い・JSON が壊れている・空ファイルは `unknown`。

参考（根拠）：

- Claude Code 自身も `expiresAt: 0` を期限切れとして扱う。<https://github.com/anthropics/claude-code/issues/83834>
- リフレッシュに失敗したあとは、トークンが空・`expiresAt: 0` のログアウト状態になる。<https://github.com/anthropics/claude-code/issues/76561>
- 長期トークン（`CLAUDE_CODE_OAUTH_TOKEN`）は `.credentials.json` に入らない。そのため `0` が「無期限」を意味することはなく、`claudeAiOauth` ごと無い場合は `unknown`（`expired` にしない）。<https://code.claude.com/docs/en/authentication>

## この機の実ファイルで確認したこと（値は記録しない）

- `~/.claude/.credentials.json`（権限 600）: `claudeAiOauth` に `accessToken`・`refreshToken`（どちらも空文字列）・`expiresAt`（数値）・`refreshTokenExpiresAt`（数値）ほか。`expiresAt` は **0**、`refreshTokenExpiresAt` は過去（2026-10-14）。トークンが空なのでログアウト状態＝**`expired`**（`unknown` ではない。`expiresAt: 0` は「1970 年に切れた」を意味し、期限不明ではなく失効の値のため）。
- `~/.codex/auth.json`: `tokens.access_token` は JWT で `exp`（数値）が過去（2026-09-05）＝**`expired`**。`last_refresh` は ISO 8601 文字列（期限ではなく最終更新）。
