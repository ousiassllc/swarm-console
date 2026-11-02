package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// DefaultTimeout は tmux 1 回の呼び出しに付ける上限（NFR-PERF）。
const DefaultTimeout = 3 * time.Second

// ErrNoServer は tmux サーバーが動いていない（ソケットに繋がらない）ことを表す。
// Client のエラーは *apperr.Error で、errors.Is(err, ErrNoServer) で判別できる。
var ErrNoServer = errors.New("tmux サーバーが動いていません")

// 許可するサブコマンド（NFR-SEC-7）。"-V" はバージョン表示。これ以外は実行しない。
const (
	subListPanes    = "list-panes"
	subCapturePane  = "capture-pane"
	subSendKeys     = "send-keys"
	subLoadBuffer   = "load-buffer"
	subPasteBuffer  = "paste-buffer"
	subDeleteBuffer = "delete-buffer"
	subSelectPane   = "select-pane"
	subDisplay      = "display"
	subVersion      = "-V"
)

var allowedSubcommands = map[string]struct{}{
	subListPanes: {}, subCapturePane: {}, subSendKeys: {}, subLoadBuffer: {},
	subPasteBuffer: {}, subDeleteBuffer: {}, subSelectPane: {}, subDisplay: {}, subVersion: {},
}

// Client は tmux コマンドを os/exec の引数配列で実行する（シェルを経由しない）。
// pane.Tmux を実装する。複数の goroutine から同時に使える。
type Client struct {
	socket  string
	bin     string
	timeout time.Duration
	pasteID pasteCounter
}

// Option は Client の設定。
type Option func(*Client)

// WithBinary は tmux 実行ファイルのパスを差し替える（既定は PATH 上の "tmux"）。
func WithBinary(path string) Option { return func(c *Client) { c.bin = path } }

// WithTimeout は 1 回の呼び出しの上限を変える（既定 3 秒）。
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.timeout = d } }

// New は Client を作る。socket が空なら既定のソケット、あれば `-L <socket>`。
func New(socket string, opts ...Option) *Client {
	c := &Client{socket: socket, bin: "tmux", timeout: DefaultTimeout}
	for _, o := range opts {
		o(c)
	}
	return c
}

// run は許可済みのサブコマンド sub を実行し、標準出力を返す。
// エラーは *apperr.Error（tmux_error）。details にはサブコマンド名だけを入れる。
func (c *Client) run(ctx context.Context, sub, stdin string, args ...string) (string, error) {
	if _, ok := allowedSubcommands[sub]; !ok {
		return "", apperr.New(apperr.Internal, "subcommand", sub)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// -u は UTF-8 の強制。LANG・LC_ALL が無い環境（systemd のユーザーサービスなど）でも
	// 日本語のタイトル・パスが "_" に化けないよう、全呼び出しの argv を作るここで必ず付ける。
	argv := make([]string, 0, len(args)+4)
	argv = append(argv, "-u")
	if c.socket != "" {
		argv = append(argv, "-L", c.socket)
	}
	argv = append(argv, sub)
	argv = append(argv, args...)

	//nolint:gosec // 実行ファイルは固定（既定 "tmux"）、引数は許可済みサブコマンド＋検証済みの値の配列でシェルを経由しない
	cmd := exec.CommandContext(ctx, c.bin, argv...)
	cmd.Env = childEnv()
	cmd.WaitDelay = 500 * time.Millisecond
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", classify(ctx, sub, err, stderr.String())
	}
	return stdout.String(), nil
}

// childEnv は TMUX・TMUX_PANE を除いた環境を返す。
// 呼び出し元がたまたま tmux の中でも、$TMUX が指すソケットではなく指定どおりのソケットを使うため。
func childEnv() []string {
	env := os.Environ()
	out := env[:0:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// classify は実行失敗を *apperr.Error にする。stderr は内部原因（Err）にだけ入れる。
func classify(ctx context.Context, sub string, err error, stderr string) error {
	msg := strings.TrimSpace(stderr)
	if len(msg) > 200 {
		msg = msg[:200]
	}
	switch {
	case ctx.Err() != nil:
		return apperr.Wrap(apperr.TmuxError, fmt.Errorf("%s: %w", sub, ctx.Err()), "subcommand", sub)
	case isNoServer(msg):
		return apperr.Wrap(apperr.TmuxError, fmt.Errorf("%s: %w: %s", sub, ErrNoServer, msg), "subcommand", sub)
	default:
		return apperr.Wrap(apperr.TmuxError, fmt.Errorf("%s: %w: %s", sub, err, msg), "subcommand", sub)
	}
}

// isNoServer は tmux の標準エラーが「サーバーに繋がらない」を表すか。
func isNoServer(msg string) bool {
	for _, p := range []string{"no server running", "error connecting to", "failed to connect to server", "no sessions", "no current"} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}
