package render

import (
	"strconv"
	"time"
)

// minuteLayout は期限・リセットの時刻（スナップショットの At のオフセットで `YYYY-MM-DD HH:MM`）。
const minuteLayout = "2006-01-02 15:04"

// words は足した値の言葉（機能要件 §5.4）。日本語と英語の 1 対 1。
type words struct {
	unavailable, pending, disabled string // 取れない・未取得・止めてある
	upToDate                       string
	updateAvail, updateAvailAfter  string // 「新しい版 X あり」の X の前後
	authValid, authExpiringSoon    string // ログインの状態の言葉
	authExpiredWord, authUnknown   string
	authExpired, authExpiring      string // 要対応の行頭
	exhausted, resets              string
	fiveHour, sevenDay             string
	account, window, used          string // 使用状況の表頭
	none                           string
	lastSuccess                    string // 「取得できず（最後の成功: …）」
	cpu, swap                      string
	listSep                        string // 更新の行の区切り
	partSep                        string // かっこの中・ディスクの区切り
	remPrefix                      string // 「あと」「in」
	unitDay, unitHour, unitMin     string
	parenOpen, parenClose          string // 補足のかっこ
	freePre, freePost              string // 「空き 120 GB」「120 GB free」
}

var jaWords = words{
	unavailable: "取得できず", pending: "未取得", disabled: "無効",
	upToDate: "最新", updateAvail: "新しい版 ", updateAvailAfter: " あり",
	authValid: "有効", authExpiringSoon: "期限間近", authExpiredWord: "期限切れ", authUnknown: "不明",
	authExpired: "🔑 ログイン期限切れ", authExpiring: "⏳ ログイン期限間近",
	exhausted: "上限到達", resets: "リセット",
	fiveHour: "5 時間枠", sevenDay: "7 日枠",
	account: "アカウント", window: "枠", used: "使用",
	none: "なし", lastSuccess: "最後の成功: ",
	cpu: "CPU", swap: "スワップ", listSep: "／ ", partSep: "、",
	remPrefix: "あと ", unitDay: " 日", unitHour: " 時間", unitMin: " 分",
	parenOpen: "（", parenClose: "）", freePre: "空き ", freePost: "",
}

var enWords = words{
	unavailable: "Unavailable", pending: "Pending", disabled: "Disabled",
	upToDate: "Up to date", updateAvail: "Update ", updateAvailAfter: " available",
	authValid: "Valid", authExpiringSoon: "Expiring soon", authExpiredWord: "Expired", authUnknown: "Unknown",
	authExpired: "🔑 Login expired", authExpiring: "⏳ Login expiring",
	exhausted: "Exhausted", resets: "Resets",
	fiveHour: "5-hour", sevenDay: "7-day",
	account: "Account", window: "Window", used: "Used",
	none: "None", lastSuccess: "last success: ",
	cpu: "CPU", swap: "Swap", listSep: " / ", partSep: ", ",
	remPrefix: "in ", unitDay: " d", unitHour: " h", unitMin: " min",
	parenOpen: " (", parenClose: ")", freePre: "", freePost: " free",
}

// remaining は期限までの時間。分未満は切り捨て：1 時間未満は「M 分」、24 時間未満は「N 時間 M 分」（M が 0 なら「N 時間」）、
// 24 時間以上は「N 日 M 時間」（M が 0 なら「N 日」）。負は 0 分。
func (w words) remaining(d time.Duration) string {
	mins := max(int(d/time.Minute), 0)
	switch {
	case mins < 60:
		return w.remPrefix + strconv.Itoa(mins) + w.unitMin
	case mins < 24*60:
		return w.remPrefix + major(mins/60, w.unitHour, mins%60, w.unitMin)
	}
	return w.remPrefix + major(mins/(24*60), w.unitDay, mins%(24*60)/60, w.unitHour)
}

// major は「N <大きい単位>」に、小さい側が 0 でなければ「 M <小さい単位>」を続ける。
func major(n int, unit string, minor int, minorUnit string) string {
	s := strconv.Itoa(n) + unit
	if minor != 0 {
		s += " " + strconv.Itoa(minor) + minorUnit
	}
	return s
}

// authWord はログインの状態の言葉。
func (w words) authWord(status string) string {
	switch status {
	case "valid":
		return w.authValid
	case "expiring_soon":
		return w.authExpiringSoon
	case "expired":
		return w.authExpiredWord
	}
	return w.authUnknown
}

// paren は補足をかっこで包む（ja は全角、en は前に空白）。
func (w words) paren(s string) string { return w.parenOpen + s + w.parenClose }
