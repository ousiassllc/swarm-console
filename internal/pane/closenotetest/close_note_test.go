// Package closenotetest は pane_close の note_archived を確かめる。
// note_archived は「この呼び出しでメモを履歴へ移したか」で、ok: true でも false になる（P-103）。
package closenotetest

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/ops"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// archiveFails は Archive だけが失敗するメモ置き場。
type archiveFails struct{ pane.NoteStore }

func (archiveFails) Archive(context.Context, notes.ArchiveMeta, string) (bool, error) {
	return false, errors.New("disk full")
}

type fixture struct {
	env  *panetest.Env
	op   *ops.Operator
	ref  pane.Ref
	logs *bytes.Buffer
}

// newFixture は omp が idle の 1 ペイン（%0・T1）を作る。終了コマンドを送るとシェルのプロンプトだけの画面に戻る。
// wrap が nil でなければ、フェイクのメモ置き場を包んで Operator に渡す。
func newFixture(t *testing.T, wrap func(pane.NoteStore) pane.NoteStore) *fixture {
	t.Helper()
	env := panetest.NewEnv()
	var store pane.NoteStore = env.Notes
	if wrap != nil {
		store = wrap(store)
	}
	logs := &bytes.Buffer{}
	reg, err := pane.NewRegistry(pane.Deps{
		Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters,
		Notes: store, Clock: env.Clock, FS: env.FS,
		Logger: slog.New(slog.NewTextHandler(logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	env.Tmux.SetPanes(env.Pane("%0", 100, 0, 0, panetest.ScreenIdle("omp"), []string{"omp"}))
	env.Tmux.OnOp(func(o panetest.Op) {
		if o.Kind == "keys" && o.Keys[0] == "Enter" {
			env.Host.SetTree(100, []string{"bash"})
			env.Tmux.SetScreen("%0", "~ $ ")
		}
	})
	return &fixture{env: env, op: ops.New(reg), ref: pane.Ref{ID: "%0", Pos: "T1"}, logs: logs}
}

// close は pane_close を 1 回呼ぶ。操作が時計（After）で待つたびに Fake の時計を進める（固定の sleep は使わない）。
func (f *fixture) close(t *testing.T) pane.CloseResult {
	t.Helper()
	type result struct {
		res pane.CloseResult
		err error
	}
	ch := make(chan result, 1)
	go func() {
		res, err := f.op.Close(context.Background(), f.ref)
		ch <- result{res, err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case r := <-ch:
			if r.err != nil {
				t.Fatal(r.err)
			}
			return r.res
		default:
		}
		if f.env.Clock.Waiters() > 0 {
			f.env.Clock.Advance(100 * time.Millisecond)
		} else {
			runtime.Gosched()
		}
		if time.Now().After(deadline) {
			t.Fatal("pane_close が終わりません")
		}
	}
}

// メモのあるペインを閉じると true。同じペインへの 2 回目は、メモがもう履歴に移っているので ok: true でも false。
func TestNoteArchivedPerCall(t *testing.T) {
	f := newFixture(t, nil)
	f.env.Notes.SetSummary("%0", notes.Summary{From: "agent-b"})
	if res := f.close(t); !res.OK || !res.NoteArchived {
		t.Fatalf("1 回目 res = %+v", res)
	}
	if res := f.close(t); !res.OK || res.NoteArchived {
		t.Fatalf("2 回目は ok: true・note_archived=false のはず: %+v", res)
	}
	if got := len(f.env.Notes.Archives()); got != 2 {
		t.Fatalf("Archive の呼び出し = %d、want 2（2 回目もメモの有無を確かめに行く）", got)
	}
}

// 閉じる前からメモがなかったペインも、ok: true・note_archived=false。
func TestNoteArchivedFalseWithoutNote(t *testing.T) {
	f := newFixture(t, nil)
	if res := f.close(t); !res.OK || res.NoteArchived {
		t.Fatalf("res = %+v", res)
	}
}

// メモを履歴へ移すのに失敗しても ok: true は変わらず、note_archived=false で WARN をログに残す。
func TestNoteArchivedFalseWhenArchiveFails(t *testing.T) {
	f := newFixture(t, func(n pane.NoteStore) pane.NoteStore { return archiveFails{n} })
	f.env.Notes.SetSummary("%0", notes.Summary{From: "agent-b"})
	if res := f.close(t); !res.OK || res.NoteArchived {
		t.Fatalf("res = %+v", res)
	}
	if log := f.logs.String(); !strings.Contains(log, "level=WARN") || !strings.Contains(log, "メモを履歴へ移せませんでした") {
		t.Fatalf("WARN が出ていない: %q", log)
	}
}
