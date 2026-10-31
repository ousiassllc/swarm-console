package render

import (
	"bytes"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// JSON のキー順は構造体の宣言順で固定（API 仕様 §4）。

type jsonHost struct {
	Load1     float64 `json:"load1"`
	MemFreeGB float64 `json:"mem_free_gb"`
}

type jsonLayout struct {
	Rows int   `json:"rows"`
	Cols []int `json:"cols"`
}

type jsonPane struct {
	Pos        string         `json:"pos"`
	ID         string         `json:"id"`
	Agent      *string        `json:"agent"`
	State      string         `json:"state"`
	Title      string         `json:"title"`
	Cwd        string         `json:"cwd"`
	Elapsed    *string        `json:"elapsed"`
	Activity   *string        `json:"activity"`
	ChangedAgo *string        `json:"changed_ago"`
	Note       *notes.Summary `json:"note"`
}

type jsonWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type jsonAttention struct {
	Kind   string `json:"kind"`
	Pane   string `json:"pane"`
	Detail string `json:"detail"`
}

type jsonStatus struct {
	At       string        `json:"at"`
	Host     jsonHost      `json:"host"`
	Layout   jsonLayout    `json:"layout"`
	Panes    []jsonPane    `json:"panes"`
	Warnings []jsonWarning `json:"warnings"`
}

type jsonReport struct {
	At        string          `json:"at"`
	Host      jsonHost        `json:"host"`
	Layout    jsonLayout      `json:"layout"`
	Panes     []jsonPane      `json:"panes"`
	Attention []jsonAttention `json:"attention"`
	Warnings  []jsonWarning   `json:"warnings"`
}

// nilIfEmpty は空文字を null にする。
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func newJSONPane(p *pane.PaneState) jsonPane {
	jp := jsonPane{
		Pos: p.Pos, ID: p.ID, Agent: nilIfEmpty(p.Agent), State: string(p.State), Title: p.Title, Cwd: p.Cwd,
		Elapsed: nilIfEmpty(p.Elapsed), Activity: nilIfEmpty(p.Activity), ChangedAgo: nilIfEmpty(p.ChangedAgo),
	}
	if p.Note != nil && !p.Note.IsZero() {
		n := *p.Note
		jp.Note = &n
	}
	return jp
}

func jsonParts(snap *pane.Snapshot) (jsonHost, jsonLayout, []jsonWarning) {
	cols := append([]int{}, snap.Layout.Cols...)
	warns := make([]jsonWarning, 0, len(snap.Warnings))
	for _, w := range snap.Warnings {
		warns = append(warns, jsonWarning{Code: w.Code, Message: w.Message})
	}
	return jsonHost{Load1: round1(snap.Host.Load[0]), MemFreeGB: round1(snap.Host.MemFreeGB)},
		jsonLayout{Rows: snap.Layout.Rows, Cols: cols}, warns
}

func jsonPanes(panes []pane.PaneState) []jsonPane {
	out := make([]jsonPane, 0, len(panes))
	for i := range panes {
		out = append(out, newJSONPane(&panes[i]))
	}
	return out
}

// marshal は HTML エスケープなしの 1 行 JSON にする。
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// PanesStatusJSON は panes_status の返り値（キー順: at, host, layout, panes, warnings）。
func PanesStatusJSON(snap *pane.Snapshot) ([]byte, error) {
	host, layout, warns := jsonParts(snap)
	return marshal(jsonStatus{At: snap.At.Format(rfc3339), Host: host, Layout: layout, Panes: jsonPanes(snap.Panes), Warnings: warns})
}

// reportJSON は report の json 形式。target があれば panes と attention をその 1 ペインに絞る。
func reportJSON(in *ReportInput, target *pane.PaneState) ([]byte, error) {
	snap := in.Snapshot
	panes := snap.Panes
	if target != nil {
		panes = []pane.PaneState{*target}
	}
	host, layout, warns := jsonParts(snap)
	att := make([]jsonAttention, 0)
	for _, a := range attentionItems(panes, in.ReportDir) {
		att = append(att, jsonAttention(a))
	}
	return marshal(jsonReport{
		At: snap.At.Format(rfc3339), Host: host, Layout: layout, Panes: jsonPanes(panes),
		Attention: att, Warnings: warns,
	})
}
