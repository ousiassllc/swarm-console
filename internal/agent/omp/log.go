package omp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

var _ agent.WorkProbe = (*adapter)(nil)

// logTailBytes はログの末尾からこれだけを読む（作業の行は新しい側にあり、ログは 1 日で数 MB になる）。
const logTailBytes = 256 << 10

// workMessages は作業（モデルの呼び出し）のときに omp がログへ書く行の message の接頭辞。
// 起動時の "Usage fetch ..." や 5 分おきの "codex-auto-reset: plan" などは、何も作業していなくても出るので数えない。
var workMessages = []string{
	"Provider in-flight limit", // モデルへの要求ごと（"... wait completed"・"... blocked request"）
	"title-generator:",         // 最初の指示を受けたあとに出る
	"jev context:",             // 会話をモデルに渡す前の整形
	"anthropic:",               // モデルの応答の整形
}

// logLine は omp のログ（1 行 1 JSON）のうち、読む項目。
type logLine struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

// LastWork は ~/.omp/logs の omp.<日付>.<pid>.log のうち、since 以降にある作業の行の最後の時刻を返す。
// 日付をまたいで同じ pid のログが複数あれば、更新が新しいほうを読む。
func (a *adapter) LastWork(pid int, since time.Time) (time.Time, bool) {
	path, mtime := a.logFile(pid)
	if path == "" || mtime.Before(since) {
		return time.Time{}, false
	}
	return lastWorkIn(path, since)
}

// logFile は pid のログファイルと、その更新時刻。無ければ ""。
func (a *adapter) logFile(pid int) (string, time.Time) {
	dir := a.logDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", time.Time{}
		}
		dir = filepath.Join(home, ".omp", "logs")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", time.Time{}
	}
	suffix := "." + strconv.Itoa(pid) + ".log"
	var best string
	var bestTime time.Time
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "omp.") || !strings.HasSuffix(name, suffix) || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) {
			best, bestTime = filepath.Join(dir, name), info.ModTime()
		}
	}
	return best, bestTime
}

// lastWorkIn は path の末尾を読み、since 以降の作業の行の最後の時刻を返す。
func lastWorkIn(path string, since time.Time) (time.Time, bool) {
	f, err := os.Open(path) //nolint:gosec // dir は ~/.omp/logs（または Adapter の作り手が渡したディレクトリ）、名前は ReadDir で選んだログだけ
	if err != nil {
		return time.Time{}, false
	}
	defer func() { _ = f.Close() }()
	if st, err := f.Stat(); err == nil && st.Size() > logTailBytes {
		if _, err := f.Seek(-logTailBytes, io.SeekEnd); err != nil {
			return time.Time{}, false
		}
	}
	var last time.Time
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, logTailBytes)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.HasPrefix(line, []byte("{")) {
			continue // 末尾の読み始めで切れた行
		}
		var l logLine
		if json.Unmarshal(line, &l) != nil || !isWorkMessage(l.Message) {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, l.Timestamp)
		if err != nil || at.Before(since) {
			continue
		}
		if at.After(last) {
			last = at
		}
	}
	return last, !last.IsZero()
}

func isWorkMessage(msg string) bool {
	for _, p := range workMessages {
		if strings.HasPrefix(msg, p) {
			return true
		}
	}
	return false
}
