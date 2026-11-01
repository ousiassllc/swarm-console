// Package tmuxtest は実 tmux を使うテスト用の補助。専用ソケット（-L・一意名）の使い捨てサーバーを立て、
// 終了時に必ず kill-server する。既定ソケット（本番の tmux）には触れない。
// テストからだけ使う（本体のコードは import しない）。
package tmuxtest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// WaitTimeout は条件待ちの上限。
const WaitTimeout = 10 * time.Second

// Server は専用ソケットの tmux サーバー。
type Server struct {
	tb     testing.TB
	Socket string // -L の名前
}

// New は一意なソケット名の Server を作り、終了時に kill-server を登録する。
// tmux が無ければテストを失敗させる（NFR-TEST-8）。サーバー自体は最初のセッションを作ったときに立ち上がる。
func New(tb testing.TB) *Server {
	tb.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		tb.Fatalf("tmux が見つかりません。tmux を使うテストには tmux が必要です（NFR-TEST-8）: %v", err)
	}
	var r [6]byte
	if _, err := rand.Read(r[:]); err != nil {
		tb.Fatalf("乱数: %v", err)
	}
	s := &Server{tb: tb, Socket: "sctest-" + hex.EncodeToString(r[:])}
	tb.Cleanup(s.close)
	return s
}

// run は専用ソケットで tmux を実行する（テスト用なので引数は呼び出し側が決める）。
func (s *Server) run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), WaitTimeout)
	defer cancel()
	//nolint:gosec // テスト専用。引数配列で実行し、シェルを経由しない
	cmd := exec.CommandContext(ctx, "tmux", append([]string{"-L", s.Socket, "-f", "/dev/null"}, args...)...)
	cmd.Env = append(os.Environ(), "SHELL=/bin/sh") // ペインのコマンドは sh 構文で書く（利用者の既定シェルに依存しない）
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), &execError{err: err, stderr: strings.TrimSpace(errb.String())}
	}
	return out.String(), nil
}

type execError struct {
	err    error
	stderr string
}

func (e *execError) Error() string { return e.err.Error() + ": " + e.stderr }
func (e *execError) Unwrap() error { return e.err }

// Run は tmux サブコマンドを実行して標準出力を返す。失敗したらテストを止める。
func (s *Server) Run(args ...string) string {
	s.tb.Helper()
	out, err := s.run(args...)
	if err != nil {
		s.tb.Fatalf("tmux %v: %v", args, err)
	}
	return out
}

// NewSession は幅 width・高さ height の使い捨てセッションを作り、最初のペインの ID（%N）を返す。
// command はシェル文字列（tmux が sh -c で実行する。テスト内の固定文字列だけを渡す）。空ならログインシェル。
func (s *Server) NewSession(name string, width, height int, command string) string {
	s.tb.Helper()
	args := []string{"new-session", "-d", "-P", "-F", "#{pane_id}", "-s", name,
		"-x", strconv.Itoa(width), "-y", strconv.Itoa(height)}
	if command != "" {
		args = append(args, command)
	}
	return strings.TrimSpace(s.Run(args...))
}

// Capture はペインの画面（履歴なし）を返す。
func (s *Server) Capture(id string) string {
	s.tb.Helper()
	return s.Run("capture-pane", "-p", "-t", id)
}

// WaitFor は cond(画面) が true になるまで、capture-pane を上限付きで繰り返す。
// 成功時の画面を返す。上限（WaitTimeout）を超えたら直近の画面付きでテストを止める。
func (s *Server) WaitFor(id string, cond func(screen string) bool) string {
	s.tb.Helper()
	deadline := time.NewTimer(WaitTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		screen, err := s.run("capture-pane", "-p", "-t", id)
		if err == nil && cond(screen) {
			return screen
		}
		select {
		case <-deadline.C:
			s.tb.Fatalf("条件待ちがタイムアウト。直近の画面:\n%s", screen)
			return screen
		case <-tick.C:
		}
	}
}

// WaitForText は画面に substr が現れるまで待つ。
func (s *Server) WaitForText(id, substr string) string {
	s.tb.Helper()
	return s.WaitFor(id, func(screen string) bool { return strings.Contains(screen, substr) })
}

// Eventually は cond が true になるまで上限（WaitTimeout）付きで繰り返す。固定時間の待ちには使わない。
func Eventually(tb testing.TB, what string, cond func() bool) {
	tb.Helper()
	deadline := time.NewTimer(WaitTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for !cond() {
		select {
		case <-deadline.C:
			tb.Fatalf("条件待ちがタイムアウト: %s", what)
			return
		case <-tick.C:
		}
	}
}

// close は専用サーバーを止め、残ったソケットファイルを消す（既定ソケットは対象外）。
func (s *Server) close() {
	path, _ := s.run("display-message", "-p", "#{socket_path}")
	_, _ = s.run("kill-server")
	if path = strings.TrimSpace(path); path != "" && strings.Contains(filepath.Base(path), "sctest-") {
		_ = os.Remove(path)
	}
}
