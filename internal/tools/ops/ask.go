package ops

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

type askPickArgs struct {
	Session *string `json:"session"`
	Pane    *string `json:"pane"`
	N       *int    `json:"n"`
	Text    *string `json:"text"`
}

type pickedJSON struct {
	N     int    `json:"n"`
	Label string `json:"label"`
}

type askPickJSON struct {
	OK         bool        `json:"ok"`
	State      string      `json:"state"`
	Picked     *pickedJSON `json:"picked,omitempty"`
	CardClosed bool        `json:"card_closed"`
}

func (e *env) askPickTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "ask_pick",
		Description: descAskPick,
		Scope:       toolkit.ScopeOps,
		Schema: toolkit.Object([]string{"pane"}, toolkit.Props{
			"pane":    toolkit.PaneProp(),
			"session": toolkit.SessionProp(),
			"n":       map[string]any{"type": "integer", "minimum": 1, "description": "選ぶ番号（1 始まり）。text と同時に指定しない"},
			"text":    toolkit.StrProp("「その他」に打つ文字（8000 字まで）。n と同時に指定しない"),
		}),
		Handler: e.askPick,
	}
}

func (e *env) askPick(ctx context.Context, c auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a askPickArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	if (a.N == nil) == (a.Text == nil) {
		return toolkit.Result{}, toolkit.InvalidArg("n")
	}
	n, text := 0, ""
	if a.N != nil {
		if n = *a.N; n < 1 {
			return toolkit.Result{}, toolkit.InvalidArg("n")
		}
	} else {
		if text = *a.Text; text == "" {
			return toolkit.Result{}, toolkit.InvalidArg("text")
		}
		if err := e.Safety.CheckText(text); err != nil {
			return toolkit.Result{}, toolkit.WithField(err, "text")
		}
	}
	ref, err := e.ResolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return toolkit.Result{}, err
	}
	res, err := e.Ops.PickAsk(ctx, ref, n, text)
	if err != nil {
		return toolkit.Result{}, apperr.As(err)
	}
	out := askPickJSON{OK: res.OK, State: string(res.State), CardClosed: res.CardClosed}
	if res.Picked != nil {
		out.Picked = &pickedJSON{N: res.Picked.N, Label: res.Picked.Label}
	}
	return toolkit.MarshalResult(out)
}
