package tmux

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

func isErrNoServer(err error) bool { return errors.Is(err, ErrNoServer) }

// listFields は list-panes の -F に並べる変数。PaneInfo の並びと対応する（parseRecord）。
var listFields = []string{
	"pane_id", "session_name", "window_index", "pane_index", "pane_top", "pane_left",
	"pane_width", "pane_height", "pane_pid", "pane_dead",
	"pane_current_command", "pane_current_path", "pane_title",
}

// ListPanes は sessions の全ペインを返す。存在しないセッションは結果に含めないだけでエラーにしない。
// サーバーが動いていなければ ErrNoServer を包んだ *apperr.Error。
// 出力はこの呼び出しだけの乱数トークンで区切るので、タイトル・パスにタブや改行が入っても崩れない。
func (c *Client) ListPanes(ctx context.Context, sessions []string) ([]PaneInfo, error) {
	if len(sessions) == 0 {
		return nil, nil
	}
	var r [8]byte
	if _, err := rand.Read(r[:]); err != nil {
		return nil, apperr.Wrap(apperr.Internal, err)
	}
	tok := "SC" + hex.EncodeToString(r[:]) + "SC"
	var f strings.Builder
	for _, name := range listFields {
		f.WriteString(tok + "#{" + name + "}")
	}
	f.WriteString(tok)

	out, err := c.run(ctx, subListPanes, "", "-a", "-F", f.String())
	if err != nil {
		return nil, err
	}
	parts := strings.Split(out, tok)
	// 先頭は "" 、以降は「フィールド × n ＋ 改行」の繰り返し。
	n := len(listFields)
	if len(parts) < 1 || (len(parts)-1)%(n+1) != 0 {
		return nil, apperr.Wrap(apperr.TmuxError, errors.New("list-panes: 出力の形が想定外"), "subcommand", subListPanes)
	}
	var panes []PaneInfo
	for i := 1; i < len(parts); i += n + 1 {
		rec := parts[i : i+n]
		if parts[i+n] != "\n" {
			return nil, apperr.Wrap(apperr.TmuxError, errors.New("list-panes: 区切りが想定外"), "subcommand", subListPanes)
		}
		if !slices.Contains(sessions, rec[1]) {
			continue
		}
		p, err := parseRecord(rec)
		if err != nil {
			return nil, apperr.Wrap(apperr.TmuxError, err, "subcommand", subListPanes)
		}
		panes = append(panes, p)
	}
	return panes, nil
}

// parseRecord は listFields の順に並んだ値を PaneInfo にする。
func parseRecord(v []string) (PaneInfo, error) {
	p := PaneInfo{ID: v[0], Session: v[1], CurrentCmd: v[10], CurrentPath: v[11], Title: v[12], Dead: v[9] == "1"}
	dst := []*int{&p.Window, &p.PaneIndex, &p.Top, &p.Left, &p.Width, &p.Height, &p.PID}
	for i, d := range dst {
		n, err := strconv.Atoi(v[2+i])
		if err != nil {
			return PaneInfo{}, errors.New("list-panes: 数値の解析に失敗: " + listFields[2+i])
		}
		*d = n
	}
	return p, nil
}
