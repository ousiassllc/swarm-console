// Package webhook は、ペインが idle になってレポートが書けたとき（pane.idle_report。FR-STATE-1〜8）と、ペインが質問で止まったとき
// （pane.asking。FR-STATE-9）に、設定した URL へ、回ごとに 1 件だけ POST する（NFR-SEC-18）。
// 送り終えた件は二度と送らない。送信の途中で止まったときだけ、再起動後に同じ件を同じ本文で送り直す（受け手は event_id で重複を捨てる）。
// URL を設定しない（既定）と何も作らず、何も送らない。URL・署名の鍵・トークンはログ・エラー文に出さない。
package webhook

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/ousiassllc/swarm-console/internal/pane"
)

// EventPaneIdleReport はイベント種別。ペインが working から idle になり、最終報告ファイルが書かれた（更新された）こと。
const EventPaneIdleReport = "pane.idle_report"

// EventPaneAsking はイベント種別。ペインが asking（質問で止まっている）になったこと。asking の回（asking を見始めてから、asking でない状態を見るまで）につき 1 件。
const EventPaneAsking = "pane.asking"

// Note は payload に載せるメモの要約（from / repo / items / next）。メモが無いときも空の値で出す（形を固定する）。
type Note struct {
	From  string   `json:"from"`
	Repo  string   `json:"repo"`
	Items []string `json:"items"`
	Next  string   `json:"next"`
}

// Payload は POST する JSON。同じイベントの再送では、本文は 1 バイトも変わらない。
type Payload struct {
	Event          string `json:"event"`
	EventID        string `json:"event_id"` // 同じペイン・同じ idle の回で同じ値。受け手が重複を見分ける
	Session        string `json:"session"`
	Pane           string `json:"pane"`    // セッション内の位置（T1 など。固定名があればそれ）
	PaneID         string `json:"pane_id"` // tmux のペイン ID（%12）
	Note           Note   `json:"note"`
	ReportPath     string `json:"report_path"`
	StateChangedAt string `json:"state_changed_at"` // working から idle に変わったのを見た時刻（RFC3339）
}

// AskingPayload は pane.asking の POST 本文。同じイベントの再送では、本文は 1 バイトも変わらない。
// 画面の内容は送らない。質問は、panes_status の activity と同じ 1 行の要約（質問カードの見出し、読めなければ `Asking …`）だけ。
type AskingPayload struct {
	Event          string `json:"event"`
	EventID        string `json:"event_id"` // 同じペイン・同じ asking の回で同じ値。受け手が重複を見分ける
	Session        string `json:"session"`
	Pane           string `json:"pane"`             // セッション内の位置（T1 など。固定名があればそれ）
	PaneID         string `json:"pane_id"`          // tmux のペイン ID（%12）
	Title          string `json:"title"`            // ペインのタイトル
	Question       string `json:"question"`         // 質問の要約（120 字まで）。無ければ ""
	StateChangedAt string `json:"state_changed_at"` // asking を見た時刻（RFC3339）
}

// askEventID は同じペインの同じ asking の回で変わらない ID。pane.idle_report の event_id と同じ時刻でも重ならない。
func askEventID(session, paneID string, at time.Time) string {
	sum := sha256.Sum256([]byte(EventPaneAsking + "\x00" + session + "\x00" + paneID + "\x00" + strconv.FormatInt(at.UnixNano(), 10)))
	return "evt_" + hex.EncodeToString(sum[:16])
}

func newAskingPayload(p *pane.PaneState, id string, at time.Time) AskingPayload {
	return AskingPayload{
		Event: EventPaneAsking, EventID: id, Session: p.Session, Pane: p.LocalPos(), PaneID: p.ID,
		Title: p.Title, Question: p.Activity, StateChangedAt: at.UTC().Format(time.RFC3339),
	}
}

// eventID は同じペインの同じ idle の回で変わらない ID。ペイン ID は tmux の再起動で使い回されるので、変わった時刻も混ぜる。
func eventID(session, paneID string, changedAt time.Time) string {
	sum := sha256.Sum256([]byte(session + "\x00" + paneID + "\x00" + strconv.FormatInt(changedAt.UnixNano(), 10)))
	return "evt_" + hex.EncodeToString(sum[:16])
}

func newPayload(reportDir string, p *pane.PaneState, e *entry) Payload {
	pl := Payload{
		Event: EventPaneIdleReport, EventID: e.EventID, Session: p.Session, Pane: p.LocalPos(), PaneID: p.ID,
		Note:           Note{Items: []string{}},
		ReportPath:     pane.ReportPath(reportDir, p.ReportPos()),
		StateChangedAt: e.ChangedAt.UTC().Format(time.RFC3339),
	}
	if n := p.Note; n != nil {
		pl.Note.From, pl.Note.Repo, pl.Note.Next = n.From, n.Repo, n.Next
		pl.Note.Items = append(pl.Note.Items, n.Items...)
	}
	return pl
}
