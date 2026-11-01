package reportvalues

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// labelRE は名前に使える文字（小文字の英数字・ハイフン・下線。長さ 32 まで）。
var labelRE = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// accountNames は同じプロバイダのメール（空は取れなかったもの）から、アカウント名を元の順で返す（report-values.md §2.2）。
// 名前はメールの `@` 以降の最初のラベルの小文字。取れなければ account-N（N はメール全体の昇順での順番、1 始まり）。
// 名前が重なったら、メール全体の昇順で 2 つ目から -2・-3 を付ける。メールのほかの部分は出さない。
func accountNames(emails []string) []string {
	order := make([]int, len(emails))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return emails[order[a]] < emails[order[b]] })
	names := make([]string, len(emails))
	seen := map[string]int{}
	for n, i := range order {
		name := domainLabel(emails[i])
		if name == "" {
			name = fmt.Sprintf("account-%d", n+1)
		}
		seen[name]++
		if seen[name] > 1 {
			name += "-" + strconv.Itoa(seen[name])
		}
		names[i] = name
	}
	return names
}

// domainLabel はメールの `@` 以降の最初のラベル（小文字）。取れない・使えない文字を含むときは ""。
func domainLabel(email string) string {
	_, domain, ok := strings.Cut(email, "@")
	if !ok {
		return ""
	}
	label, _, _ := strings.Cut(domain, ".")
	label = strings.ToLower(strings.TrimSpace(label))
	if !labelRE.MatchString(label) {
		return ""
	}
	return label
}
