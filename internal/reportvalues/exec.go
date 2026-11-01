package reportvalues

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Command は実行を許す 2 つのコマンド（NFR-SEC-7）。引数は下の表で固定で、呼び出し側から渡せない。
type Command int

const (
	// CmdUsage は `omp usage --json`。
	CmdUsage Command = iota + 1
	// CmdUpdateCheck は `omp update --check`（`omp update` は実行しない）。
	CmdUpdateCheck
)

// fixedArgs は各コマンドの引数。これ以外の引数で omp を実行する経路は無い。
var fixedArgs = map[Command][]string{
	CmdUsage:       {"usage", "--json"},
	CmdUpdateCheck: {"update", "--check"},
}

// 打ち切りと出力の上限（report-values.md §2.1・§4）。
const (
	DefaultUsageTimeout = 60 * time.Second
	DefaultCheckTimeout = 30 * time.Second
	MaxOutput           = 2 << 20
	killWait            = 2 * time.Second
)

// Runner はコマンドを実行して標準出力を返す。失敗は reason（Reason*）だけで返す（出力・エラー文は持ち出さない）。
type Runner interface {
	Run(ctx context.Context, c Command) (out []byte, reason string)
}

// ExecRunner は実行ファイルを直接 exec する Runner（シェルなし・標準入力 /dev/null・tty なし・打ち切り・出力上限）。
type ExecRunner struct {
	// Command は `[agents.omp] command` の値。先頭の語だけを実行ファイルに使う（空は omp）。
	Command string
	// Home は `~` と ~/.local/bin の基準。
	Home string
	// UsageTimeout・CheckTimeout は 0 なら既定（テスト用に変えられる）。
	UsageTimeout, CheckTimeout time.Duration
}

var _ Runner = (*ExecRunner)(nil)

// Run は c を実行する。
func (r *ExecRunner) Run(ctx context.Context, c Command) ([]byte, string) {
	args, ok := fixedArgs[c]
	if !ok {
		return nil, ReasonExec
	}
	path := r.pathEnv()
	exe, found := r.lookup(path)
	if !found {
		return nil, ReasonNotFound
	}
	timeout := r.UsageTimeout
	if c == CmdUpdateCheck {
		timeout = r.CheckTimeout
	}
	if timeout <= 0 {
		timeout = DefaultUsageTimeout
		if c == CmdUpdateCheck {
			timeout = DefaultCheckTimeout
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...) //nolint:gosec // 実行ファイルは設定の先頭の語、引数は fixedArgs の固定値だけ（NFR-SEC-7）
	cmd.Env = append(os.Environ(), "PATH="+path)
	// Stdin・Stderr は nil（/dev/null）。tty は使わない。
	buf := &limitBuffer{max: MaxOutput}
	cmd.Stdout = buf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) } // 子プロセスも止める
	cmd.WaitDelay = killWait
	err := cmd.Run()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, ReasonTimeout
	case buf.over:
		return nil, ReasonExec
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return nil, ReasonNotFound
	case err != nil:
		return nil, ReasonExec
	}
	return buf.Bytes(), ""
}

// pathEnv は PATH に ~/.local/bin を足したもの。
func (r *ExecRunner) pathEnv() string {
	p := os.Getenv("PATH")
	if r.Home == "" {
		return p
	}
	local := filepath.Join(r.Home, ".local", "bin")
	if p == "" {
		return local
	}
	return p + string(os.PathListSeparator) + local
}

// lookup は実行ファイルの場所。絶対パスならそのまま、そうでなければ path の各フォルダから探す。
func (r *ExecRunner) lookup(path string) (string, bool) {
	name := "omp"
	if f := strings.Fields(r.Command); len(f) > 0 {
		name = f[0]
	}
	if rest, ok := strings.CutPrefix(name, "~/"); ok && r.Home != "" {
		name = filepath.Join(r.Home, rest)
	}
	if filepath.IsAbs(name) {
		return name, isExecutable(name)
	}
	if strings.ContainsRune(name, filepath.Separator) {
		return "", false // 相対パスは探さない
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		if p := filepath.Join(dir, name); isExecutable(p) {
			return p, true
		}
	}
	return "", false
}

func isExecutable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0
}

// limitBuffer は max バイトまでためる。超えたら over を立てて書き込みを失敗させる（コマンドは SIGPIPE で止まる）。
type limitBuffer struct {
	buf  bytes.Buffer // 埋め込まない（io.Copy が ReadFrom を使うと上限を素通りする）
	max  int
	over bool
}

// Bytes はためた出力。
func (b *limitBuffer) Bytes() []byte { return b.buf.Bytes() }

func (b *limitBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > b.max {
		b.over = true
		return 0, errors.New("出力が上限を超えました")
	}
	return b.buf.Write(p)
}
