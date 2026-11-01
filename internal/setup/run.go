// Package setup は対話型の設定（`swarm-console setup`。FR-SET）を持つ。
// 画面（huh）、tailnet 候補の列挙、入力 → 編集操作の変換、保存（検査・競合検出・原子的書き込み）。
// config.toml を壊さない規則は ER §5.1。端末かどうかの判定は cmd/swarm-console/setup.go が行う。
package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	_ "github.com/ousiassllc/swarm-console/internal/agent/allagents" // 入力欄の既定値（組み込みアダプタの起動・終了コマンド）を引く

	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/config/tomledit"
)

// RestartHint は保存後の案内（再起動は実行しない。NFR-OPS-3）。
const RestartHint = "systemctl --user restart swarm-console"

// Options は Run の設定。
type Options struct {
	Path     string // config.toml のパス
	Out      io.Writer
	Home     string
	Prompter Prompter
	// IssueToken は `token add` と同じ処理でトークンを発行する（平文は標準出力へ 1 度だけ）。保存のあとに呼ぶ。
	IssueToken func(name string, scope auth.Scope) error
}

// Run は setup の流れ全体（FR-SET §11.2）。TTY の判定は呼び出し側。
// 中止は ErrAborted。どの失敗でも、config.toml は保存前のまま（書き込みは 1 回で、検査をすべて通ったあとだけ）。
func Run(ctx context.Context, o Options) error {
	st, err := Load(o.Path)
	if err != nil {
		return err
	}
	ans, err := o.Prompter.Ask(ctx, st)
	if err != nil {
		return err
	}
	ops, err := confirmCommentLoss(ctx, o.Prompter, st, Diff(st, ans.Values, o.Home))
	if err != nil {
		return err
	}
	out, err := Render(st, ops)
	if err != nil {
		return err
	}
	if err := report(o, st, ops, out); err != nil {
		return err
	}
	if ans.Token == nil {
		return nil
	}
	if err := o.IssueToken(ans.Token.Name, ans.Token.Scope); err != nil {
		return fmt.Errorf("トークンを発行できません（保存した config.toml はそのままです）: %w", err)
	}
	return nil
}

// report は保存して結果を表示する。変更が無ければ書かない。
func report(o Options, st *State, ops []tomledit.Op, out []byte) error {
	if Unchanged(st, out) {
		_, err := fmt.Fprintf(o.Out, "変更はありません（%s は書き換えていません）。\n", st.Path)
		return err
	}
	if err := Save(st, out); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(o.Out, "保存しました: %s\n変更したキー:\n", st.Path); err != nil {
		return err
	}
	for _, k := range Keys(ops) {
		if _, err := fmt.Fprintf(o.Out, "  - %s\n", k); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(o.Out, "反映するには次を実行してください（setup は再起動しません）:\n  %s\n", RestartHint)
	return err
}

// confirmCommentLoss は、コメント入りの複数行配列を置き換える操作だけ確認する。了承されなければその操作を外す。
func confirmCommentLoss(ctx context.Context, p Prompter, st *State, ops []tomledit.Op) ([]tomledit.Op, error) {
	keys := make([][]string, len(ops))
	for i, op := range ops {
		keys[i] = op.Key
	}
	risky, err := tomledit.CommentedArrays(st.Src, keys)
	if err != nil {
		return nil, err
	}
	if len(risky) == 0 {
		return ops, nil
	}
	var out []tomledit.Op
	for _, op := range ops {
		if !isRisky(risky, op.Key) {
			out = append(out, op)
			continue
		}
		ok, err := p.ConfirmCommentLoss(ctx, strings.Join(op.Key, "."))
		if err != nil {
			return nil, err
		}
		if ok {
			op.AllowCommentLoss = true
			out = append(out, op)
		}
	}
	return out, nil
}

func isRisky(risky [][]string, key []string) bool {
	for _, r := range risky {
		if strings.Join(r, ".") == strings.Join(key, ".") {
			return true
		}
	}
	return false
}

// IsAbort は中止（Ctrl+C・Esc）か。
func IsAbort(err error) bool { return errors.Is(err, ErrAborted) }
