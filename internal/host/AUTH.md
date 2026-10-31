# 認証期限の読み先（P-16）

読むのは **期限だけ**。トークン文字列・メールなどは返り値・ログ・エラーに出さない。ファイルが無い・壊れている・形式違いは `unknown`（エラーにしない）。`ExpiresAt` が 48 時間以内なら `expiring_soon`、過ぎていれば `expired`、それ以外 `valid`。

| agent | 読み先 | 読むキー | 判断 |
|-------|--------|----------|------|
| claude | `~/.claude/.credentials.json` | `claudeAiOauth.expiresAt`（epoch ミリ秒）・`claudeAiOauth.refreshTokenExpiresAt`（同） | 2 つのうち遅いほうを期限とする（アクセストークンは数時間で切れ、更新トークンで自動更新されるため、「ログインが生きている」期限は更新トークン側）。両方が 0 以下（または `expiresAt` が 0 で更新側が無い）なら、トークンが消された状態（ログアウト）として `expired`・期限なし。`expiresAt` が無い・数値でないなら `unknown` |
| codex | `~/.codex/auth.json` | `tokens.access_token`（JWT）の payload の `exp`（epoch 秒）だけ | `exp` を期限とする。JWT でない・`exp` が無いなら `unknown`。`last_refresh` は読まない（期限は `exp` が正） |
| omp | 設定 `[agents.omp] auth_file` | 最上位の `expires_at`・`expiresAt`・`exp` のうち最初に見つかったもの | 数値は `1e12` 以上ならミリ秒、未満なら秒。文字列は RFC 3339。0 以下・無し・形式違いは `unknown`。`auth_file` が未設定なら `unknown` |

どのエージェントも `auth_file` を設定すればそちらを（`~/` 展開して）読む。形式はそのエージェントの上の行と同じ。

## この機の実ファイルで確認したこと（値は記録しない）

- `~/.claude/.credentials.json`（権限 600）: `claudeAiOauth` に `accessToken`・`refreshToken`（どちらも空文字列）・`expiresAt`（数値）・`refreshTokenExpiresAt`（数値）ほか。`expiresAt` は **0**、`refreshTokenExpiresAt` は過去（2026-10-14）。トークンが空なのでログアウト状態＝**`expired`**（`unknown` ではない。`expiresAt: 0` は「1970 年に切れた」を意味し、期限不明ではなく失効の値のため）。
- `~/.codex/auth.json`: `tokens.access_token` は JWT で `exp`（数値）が過去（2026-09-05）＝**`expired`**。`last_refresh` は ISO 8601 文字列（期限ではなく最終更新）。

## 仕様書への反映案

P-16 の「実装時（実ファイル形式）」を確定にし、上の表の読み先・判断を書く。
