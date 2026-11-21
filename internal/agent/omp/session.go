package omp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/omp/transcript"
)

var _ agent.TurnProbe = (*adapter)(nil)

// セッションの記録の末尾から読む量の下限。上限は transcript.TailMax。1 行（ツールの結果）が数十〜百 KB になることがあるので、見つからなければ上限まで倍に広げる。
const sessionTailMin = 64 << 10

// sessionMessage は omp のセッションの記録（1 行 1 JSON）のうち、やり取り（"type":"message"）の読む項目。
type sessionMessage struct {
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role       string `json:"role"`
		StopReason string `json:"stopReason"`
	} `json:"message"`
}

var messagePrefix = []byte(`{"type":"message"`)

// Turn は pid の omp が開いているセッションの記録（~/.omp/agent/sessions/<作業ディレクトリ>/<日時>_<id>.jsonl）の、since 以降の最後のやり取りを返す。
// omp はユーザーの指示・モデルの応答・ツールの結果を 1 つずつ追記する。ツールの実行中（長いコマンド）は、最後がツールの呼び出し（stopReason=toolUse）のまま、結果が書かれるまで動かない。
func (a *adapter) Turn(pid int, since time.Time) (last time.Time, inTurn, ok bool) {
	path := a.sessionFile(pid)
	if path == "" {
		return time.Time{}, false, false
	}
	return lastTurnIn(path, since)
}

// sessionFile は pid の omp が開いているセッションの記録。無ければ ""。
// 開いているファイルは /proc/<pid>/fd で分かる。親のセッションの直下の記録だけを選ぶ（助言役の __advisor.jsonl やサブエージェントの記録は、セッションの名前のフォルダの中にある）。
func (a *adapter) sessionFile(pid int) string {
	procDir, root := a.procDir, a.sessionsDir
	if procDir == "" {
		procDir = "/proc"
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		root = filepath.Join(home, ".omp", "agent", "sessions")
	}
	fdDir := filepath.Join(procDir, strconv.Itoa(pid), "fd")
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		return ""
	}
	var best string
	var bestTime time.Time
	for _, fd := range fds {
		target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
		if err != nil || !isSessionFile(root, target) {
			continue
		}
		info, err := os.Stat(target)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) {
			best, bestTime = target, info.ModTime()
		}
	}
	return best
}

// isSessionFile は path が root/<フォルダ>/<名前>.jsonl の形（__ で始まる補助の記録でない）かを返す。
func isSessionFile(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 || parts[0] == ".." {
		return false
	}
	return strings.HasSuffix(parts[1], ".jsonl") && !strings.HasPrefix(parts[1], "__")
}

// lastTurnIn は path の末尾から後ろ向きに読み、since 以降の最後のやり取りで番が終わっているかを返す。
// ユーザー（user）・ツールの結果（toolResult）・ツールの呼び出し（assistant で stopReason=toolUse）は番の途中。
// モデルの最終の応答（stop）・中断（aborted）・失敗（error）は番の終わり。それ以外の役（developer・custom）は番の判定に使わない。
// 末尾 sessionTailMin から読み始め、やり取りが見つからなければ倍にして transcript.TailMax まで広げる（ツールの結果の 1 行が数十 KB になるため）。
func lastTurnIn(path string, since time.Time) (last time.Time, inTurn, ok bool) {
	f, err := os.Open(path) //nolint:gosec // 名前は /proc/<pid>/fd が指す、~/.omp/agent/sessions の下のセッションの記録だけ
	if err != nil {
		return time.Time{}, false, false
	}
	defer func() { _ = f.Close() }()
	var found bool
	err = transcript.GrowTail(f, sessionTailMin, transcript.TailMax, func(buf []byte, _ bool) bool {
		last, inTurn, ok, found = lastTurnInTail(buf, since)
		return found
	})
	if err != nil {
		return time.Time{}, false, false
	}
	return last, inTurn, ok
}

// lastTurnInTail は buf（末尾の窓の中の完全な行）からやり取りを探す。found=false は、読んだ範囲に見つからず、もっと前を読む価値があること。
func lastTurnInTail(buf []byte, since time.Time) (last time.Time, inTurn, ok, found bool) {
	for end := len(buf); end > 0; {
		start := bytes.LastIndexByte(buf[:end], '\n') + 1
		line := buf[start:end]
		end = start - 1
		if !bytes.HasPrefix(line, messagePrefix) {
			continue
		}
		var m sessionMessage
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, m.Timestamp)
		if err != nil {
			continue
		}
		if at.Before(since) {
			return time.Time{}, false, false, true // 以降のやり取りは無い（古い番の記録だけ）
		}
		switch m.Message.Role {
		case "user", "toolResult":
			return at, true, true, true
		case "assistant":
			return at, m.Message.StopReason == "toolUse", true, true
		}
	}
	return time.Time{}, false, false, false
}
