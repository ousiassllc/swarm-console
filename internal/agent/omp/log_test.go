package omp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Issue #113: omp のログ（~/.omp/logs/omp.<日付>.<pid>.log）から、作業の跡の最後の時刻を読む。

func entry(ts, msg string) string {
	return `{"timestamp":"` + ts + `","level":"debug","pid":1,"message":"` + msg + `","provider":"anthropic"}` + "\n"
}

func writeLog(t *testing.T, dir, name string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "")), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLastWorkReadsModelCallsOnly(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "omp.2026-11-20.4242.log",
		entry("2026-11-20T15:10:36.606+09:00", "Usage fetch requested"),
		entry("2026-11-20T15:10:41.065+09:00", "Provider in-flight limit wait completed"),
		entry("2026-11-20T15:11:00.192+09:00", "Provider in-flight limit blocked request"),
		"not json\n",
		entry("2026-11-20T15:15:37.637+09:00", "Usage fetch queued"),
		entry("2026-11-20T15:15:38.087+09:00", "codex-auto-reset: plan"),
	)
	a := &adapter{logDir: dir}
	since := time.Date(2026, 11, 20, 15, 10, 48, 0, time.FixedZone("", 9*3600))

	at, ok := a.LastWork(4242, since)
	want := time.Date(2026, 11, 20, 15, 11, 0, 192_000_000, time.FixedZone("", 9*3600))
	if !ok || !at.Equal(want) {
		t.Fatalf("LastWork = %v, %v, want %v（定期の Usage fetch などは数えない）", at, ok, want)
	}
	if _, ok := a.LastWork(4242, want.Add(time.Millisecond)); ok {
		t.Error("since より後に作業の行が無いのに ok")
	}
}

// 起動中の行や定期の更新だけのログは、作業の跡ではない（起動したまま止まっているペインを working にしない）。
func TestLastWorkIgnoresIdleNoise(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "omp.2026-11-20.77.log",
		entry("2026-11-20T15:10:36.606+09:00", "Usage fetch requested"),
		entry("2026-11-20T15:10:37.100+09:00", "claude-auto-reset: plan"),
		entry("2026-11-20T15:10:38.094+09:00", "MCP prompt commands refreshed"),
	)
	if at, ok := (&adapter{logDir: dir}).LastWork(77, time.Time{}); ok {
		t.Fatalf("作業していないログで ok: %v", at)
	}
}

// pid が違うログ（末尾が同じ数字で終わる別の pid を含む）は読まない。ログが無い・ディレクトリが無いときは ok=false。
func TestLastWorkMatchesExactPid(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "omp.2026-11-20.1242.log", entry("2026-11-20T15:11:00.000+09:00", "Provider in-flight limit wait completed"))
	a := &adapter{logDir: dir}
	if _, ok := a.LastWork(242, time.Time{}); ok {
		t.Error("別の pid（1242）のログを読んだ")
	}
	if _, ok := a.LastWork(1242, time.Time{}); !ok {
		t.Error("pid 1242 のログを読めない")
	}
	if _, ok := (&adapter{logDir: filepath.Join(dir, "none")}).LastWork(1242, time.Time{}); ok {
		t.Error("ディレクトリが無いのに ok")
	}
}

// 大きなログは末尾だけを読む。先頭の古い作業の行は見ず、末尾の新しい行を拾う。
func TestLastWorkReadsOnlyTailOfLargeLog(t *testing.T) {
	dir := t.TempDir()
	filler := strings.Repeat(entry("2026-11-20T15:00:00.000+09:00", "Usage fetch queued"), logTailBytes/90)
	writeLog(t, dir, "omp.2026-11-20.9.log",
		entry("2026-11-20T14:00:00.000+09:00", "Provider in-flight limit wait completed"), // 先頭（読まない）
		filler,
		entry("2026-11-20T15:30:00.000+09:00", "Provider in-flight limit wait completed"),
	)
	at, ok := (&adapter{logDir: dir}).LastWork(9, time.Time{})
	if !ok || at.Hour() != 15 || at.Minute() != 30 {
		t.Fatalf("末尾の行を拾えない: %v %v", at, ok)
	}
}
