// fakeagent は omp・Claude Code・Codex の画面を実画面サンプルで再現する擬似エージェント（コンポーネント設計 §6.1）。
//
// 使い方:
//
//	omp <scenario>                      （argv[0] が omp・claude・codex のとき。PATH に同名で置く）
//	fakeagent <agent> <scenario>        （それ以外の名前で起動したとき）
//	  [--tick 1s] [--log <path>] [--raw <path>]
//
// scenario は idle・working・ask・ask-custom・ask-frozen（質問カードを出したまま入力に一切反応しない。
// --raw に受け取ったバイト列をそのまま追記するので、cursor_mismatch のとき Enter が届かないことを確かめられる）。画面は 120 桁 × 40 行以上の端末を前提にする。
// 起動して最初の画面を描くと、画面の先頭行に READY を出す（テストは capture-pane で待つ）。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var (
	agents    = []string{"omp", "claude", "codex"}
	scenarios = []string{"idle", "working", "ask", "ask-custom", "ask-frozen"}
)

type options struct {
	agent, scenario, log string
	raw                  string // 受け取った入力をそのまま追記するファイル（空なら記録しない）
	tick                 time.Duration
}

func main() {
	if err := run(os.Args, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "fakeagent:", err)
		os.Exit(2)
	}
}

func parseArgs(args []string) (options, error) {
	o := options{}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.DurationVar(&o.tick, "tick", time.Second, "working のスピナーのコマを変える間隔")
	fs.StringVar(&o.log, "log", "", "貼り付け・選択などを追記するファイル")
	fs.StringVar(&o.raw, "raw", "", "受け取った入力のバイト列をそのまま追記するファイル")
	// フラグは位置引数の前後どちらにも書ける（`omp ask --tick 50ms`）。
	var pos []string
	for rest := args[1:]; ; {
		if err := fs.Parse(rest); err != nil {
			return o, err
		}
		if fs.NArg() == 0 {
			break
		}
		pos, rest = append(pos, fs.Arg(0)), fs.Args()[1:]
	}
	if name := filepath.Base(args[0]); slices.Contains(agents, name) {
		o.agent = name
	} else if len(pos) > 0 {
		o.agent, pos = pos[0], pos[1:]
	}
	if len(pos) != 1 {
		return o, errors.New("使い方: <omp|claude|codex> <idle|working|ask|ask-custom|ask-frozen> [--tick d] [--log path] [--raw path]")
	}
	o.scenario = pos[0]
	switch {
	case !slices.Contains(agents, o.agent):
		return o, fmt.Errorf("アダプタ種別 %q は omp・claude・codex のどれか", o.agent)
	case !slices.Contains(scenarios, o.scenario):
		return o, fmt.Errorf("シナリオ %q は %s のどれか", o.scenario, strings.Join(scenarios, "・"))
	case o.agent == "codex" && o.scenario == "ask-custom":
		return o, errors.New("codex の承認カードに自由入力の項はないので ask-custom はありません")
	case o.tick <= 0:
		return o, errors.New("--tick は正の長さ")
	}
	return o, nil
}

func run(args []string, stdin *os.File, stdout io.Writer) error {
	o, err := parseArgs(args)
	if err != nil {
		return err
	}
	restore, err := makeRaw(int(stdin.Fd()))
	if err != nil {
		return err
	}
	defer restore()
	var logErr error
	m := newMachine(o.agent, o.scenario, func(e event) {
		if err := appendLog(o.log, e); err != nil && logErr == nil {
			logErr = err
		}
	})
	// 貼り付けを括る ESC[200~ … ESC[201~ を tmux が送るには、アプリが ESC[?2004h を出している必要がある。
	_, _ = io.WriteString(stdout, "\x1b[?2004h")
	defer func() { _, _ = io.WriteString(stdout, "\x1b[?2004l\r\n") }()
	if err := draw(m, stdout); err != nil {
		return err
	}
	input := make(chan []byte)
	go readLoop(stdin, input)
	ticker := time.NewTicker(o.tick)
	defer ticker.Stop()
	for !m.quit {
		select {
		case b, ok := <-input:
			if !ok {
				return logErr
			}
			if err := appendRaw(o.raw, b); err != nil && logErr == nil {
				logErr = err
			}
			m.Feed(b)
		case <-ticker.C:
			if !m.Tick() {
				continue
			}
		}
		if m.quit {
			break
		}
		if err := draw(m, stdout); err != nil {
			return err
		}
	}
	return logErr
}

func readLoop(r io.Reader, out chan<- []byte) {
	defer close(out)
	buf := make([]byte, 64*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out <- append([]byte(nil), buf[:n]...)
		}
		if err != nil {
			return
		}
	}
}

// draw は画面を消して現在のフレームを 1 回の書き込みで描く（READY が見えれば全体が描けている）。
func draw(m *machine, w io.Writer) error {
	lines, err := m.Frame()
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, "\x1b[H\x1b[2J"+strings.Join(lines, "\r\n"))
	return err
}
