package safety

import "regexp"

// secretRe は P-14 の秘密文字列のパターン。キーの種類の先頭は直前が英数字でないときだけ
// （"task-run-integration-tests" の "sk-…" のようなハイフン区切りの語を秘密と誤判定しない）。
//
//	GitHub トークン: gh[pousr]_ ＋ 20 字以上、github_pat_ ＋ 英数字
//	OpenAI 系: sk- ＋ 16 字以上　JWT: eyJ ＋ 20 字以上　AWS: AKIA ＋ 16 字
//	PEM 秘密鍵の見出し　自分のトークン: sc_ ＋ 40 字以上の base64url
var secretRe = regexp.MustCompile(
	`(?:^|[^A-Za-z0-9])(?:` +
		`gh[pousr]_[A-Za-z0-9_]{20,}` +
		`|github_pat_[A-Za-z0-9_]+` +
		`|sk-[A-Za-z0-9_-]{16,}` +
		`|eyJ[A-Za-z0-9_-]{20,}` +
		`|AKIA[0-9A-Z]{16}` +
		`|sc_[A-Za-z0-9_-]{40,}` +
		`)` +
		`|-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----`)

// DetectSecret は s に秘密に見える文字列が含まれるか返す。見つけた値は返さない（ログ・応答に出さないため）。
func DetectSecret(s string) bool { return secretRe.MatchString(s) }
