package safety

import (
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/config"
)

func nil2() *config.Config { return &config.Config{} }

func TestDetectSecret(t *testing.T) {
	a := func(n int) string { return strings.Repeat("a", n) }
	// 値はすべてダミー（実在のトークンではない）。
	//nolint:gosec // 検出器のテスト用のダミー（PEM の見出し行だけで鍵の中身ではない）
	positive := map[string]string{
		"ghp 20 字":   "ghp_" + a(20),
		"gho":        "gho_" + a(25),
		"ghu":        "ghu_" + a(20),
		"ghs":        "ghs_" + a(20),
		"ghr":        "ghr_" + a(20),
		"ghp 英数字混在":  "token: ghp_1A2b3C4d5E6f7G8h9I0jK1l2M3n4",
		"github_pat": "github_pat_11ABCDEFG0abcdef_ghijklmnop",
		"sk- 16 字":   "sk-" + a(16),
		"sk- ハイフン・アンダースコア入り": "sk-ant-api03-ab_cd-" + a(10),
		"eyJ 20 字":      "eyJ" + a(20),
		"JWT 全体":        "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dummysig",
		"AKIA 16 字":     "AKIA" + strings.Repeat("A1", 8),
		"PEM RSA":       "-----BEGIN RSA PRIVATE KEY-----\nMIIB",
		"PEM 無印":        "-----BEGIN PRIVATE KEY-----",
		"PEM EC":        "-----BEGIN EC PRIVATE KEY-----",
		"PEM OPENSSH":   "-----BEGIN OPENSSH PRIVATE KEY-----",
		"PEM ENCRYPTED": "-----BEGIN ENCRYPTED PRIVATE KEY-----",
		"自分のトークン 43 字":  "sc_" + strings.Repeat("Ab-_", 10) + "xyz",
		"自分のトークン ちょうど 40 字": "sc_" + strings.Repeat("Ab-_", 10),
		"文中（前が空白）":          "使うのは ghp_" + a(30) + " です",
		"文中（前が記号）":          "Authorization: Bearer eyJ" + a(30),
		"引用符の中":             `"sk-` + a(20) + `"`,
		"行頭":                "sk-" + a(16),
		"複数行の途中":            "line1\nline2 AKIA" + strings.Repeat("A", 16) + "\nline3",
	}
	for name, s := range positive {
		if !DetectSecret(s) {
			t.Errorf("検出のはず: %s: %q", name, s)
		}
	}
	negative := map[string]string{
		"ghp 19 字":          "ghp_" + a(19),
		"gh_ 種別違い":          "ghx_" + a(30),
		"ghp の前が英数字":        "xghp_" + a(30),
		"sk- 15 字":          "sk-" + a(15),
		"task-... の途中の sk-": "task-run-integration-tests-for-login",
		"risk-assessment":   "risk-assessment-of-the-whole-system",
		"ask-ai-agent":      "branch ask-ai-agent-for-everything",
		"eyJ 19 字":          "eyJ" + a(19),
		"eyJ の前が英数字":        "xeyJ" + a(30),
		"AKIA 15 字":         "AKIA" + strings.Repeat("A", 15),
		"AKIA の前が英数字":       "XAKIA" + strings.Repeat("A", 16),
		"AKIA 小文字":          "AKIA" + strings.Repeat("a", 16),
		"PEM 公開鍵":           "-----BEGIN PUBLIC KEY-----",
		"PEM 証明書":           "-----BEGIN CERTIFICATE-----",
		"PEM の文言だけ":         "PRIVATE KEY の扱い",
		"sc_ 39 字":          "sc_" + a(39),
		"sc_ に使えない文字":       "sc_" + a(20) + "!" + a(20),
		"sc_ の前が英数字":        "disc_" + a(45),
		"github_pat_ だけ":    "github_pat_ の形式は使わない",
		"プレフィックス名だけ":        "ghp_・sk-・eyJ・AKIA・sc_ で始まる文字列",
		"通常の文章":             "PR #982 を CI 待ち。次は #983 を入れる。",
		"空":                 "",
		"パス":                "/mnt/nvme/ousiassllc/funid-sweep-982",
		"ブランチ名":             "sweep/issue-sweep-20261031-193532",
		"git の SHA":         "a94a8fe5ccb19ba61c4c0873d391e987982fbbd3",
	}
	for name, s := range negative {
		if DetectSecret(s) {
			t.Errorf("検出しないはず: %s: %q", name, s)
		}
	}
	// Policy 越しも同じ。
	if !NewPolicy(nil2()).DetectSecret("sk-"+a(20)) || NewPolicy(nil2()).DetectSecret("hello") {
		t.Error("Policy.DetectSecret が DetectSecret と違う")
	}
}
