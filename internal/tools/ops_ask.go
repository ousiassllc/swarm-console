package tools

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
)

type askPickArgs struct {
	Pane *string `json:"pane"`
	N    *int    `json:"n"`
	Text *string `json:"text"`
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

func (e *env) askPickTool() Tool {
	return Tool{
		Name:        "ask_pick",
		Description: descAskPick,
		Scope:       ScopeOps,
		Schema: object([]string{"pane"}, props{
			"pane": paneProp(),
			"n":    map[string]any{"type": "integer", "minimum": 1, "description": "選ぶ番号（1 始まり）。text と同時に指定しない"},
			"text": strProp("「その他」に打つ文字（8000 字まで）。n と同時に指定しない"),
		}),
		Handler: e.askPick,
	}
}

func (e *env) askPick(ctx context.Context, _ auth.Client, raw json.RawMessage) (Result, error) {
	var a askPickArgs
	if err := decodeArgs(raw, &a); err != nil {
		return Result{}, err
	}
	if (a.N == nil) == (a.Text == nil) {
		return Result{}, invalidArg("n")
	}
	n, text := 0, ""
	if a.N != nil {
		if n = *a.N; n < 1 {
			return Result{}, invalidArg("n")
		}
	} else {
		if text = *a.Text; text == "" {
			return Result{}, invalidArg("text")
		}
		if err := e.Safety.CheckText(text); err != nil {
			return Result{}, withField(err, "text")
		}
	}
	ref, err := resolvePane(ctx, e.Registry, a.Pane)
	if err != nil {
		return Result{}, err
	}
	res, err := e.Ops.PickAsk(ctx, ref, n, text)
	if err != nil {
		return Result{}, apperr.As(err)
	}
	out := askPickJSON{OK: res.OK, State: string(res.State), CardClosed: res.CardClosed}
	if res.Picked != nil {
		out.Picked = &pickedJSON{N: res.Picked.N, Label: res.Picked.Label}
	}
	return marshalResult(out)
}
