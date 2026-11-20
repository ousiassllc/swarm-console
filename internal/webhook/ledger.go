package webhook

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"

	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/store"
)

// ペインごとの回（ラウンド）の段階。送った・諦めたの記録は、回ではなく outbox の項目が持つ。
const (
	phaseWorking = "working" // working を見た。Base が、そのとき見た最終報告ファイル
	phaseWaiting = "waiting" // working から idle になった。最終報告が Base から変わるのを待つ（まだ outbox に入れていない）
	phaseDone    = "done"    // 最終報告が書かれて outbox に入れた。次に working を見るまで、この回は二度と始めない
)

// reportStat は最終報告ファイルの様子。書かれた・更新されたかは、working を見たときの値と変わったかで決める。
type reportStat struct {
	Exists bool  `json:"exists"`
	ModNS  int64 `json:"mtime_ns,omitempty"`
	Size   int64 `json:"size,omitempty"`
}

// entry はペイン 1 つの現在の回。台帳（webhook-state.json）に書き、再起動しても同じ回を続けて二重に作らない。
type entry struct {
	Phase     string     `json:"phase"`
	EventID   string     `json:"event_id,omitempty"`
	ChangedAt time.Time  `json:"state_changed_at"`
	Base      reportStat `json:"report_base"`
	BasePath  string     `json:"report_path"` // Base を取った最終報告のパス。ペインの位置が変わって違うファイルになったら、取り直す
	// Sent は done にした（件を作った）ときの最終報告の様子。次の回の Base にする（Issue #115）。
	// 件を作ってから次の working を見るまでに最終報告が書き直されても、その回の idle で「書けた」と読めるようにするため。
	// 無い（#115 より前の台帳の done）ときは、次の working を見たときの最終報告を Base にする（今までどおり）。
	Sent *reportStat `json:"report_sent,omitempty"`
}

// item は送る約束をした 1 件（outbox）。回の状態とは独立で、ペインが working に戻っても・消えても・tmux が再起動しても、
// 送り終える（または諦める）まで台帳に残り、本文はそのまま（同じ署名で）送る。
type item struct {
	EventID  string          `json:"event_id"`
	Payload  json.RawMessage `json:"payload"`  // 送る本文そのもの。再送も再起動後も、この 1 バイトも変えずに送る
	Attempts int             `json:"attempts"` // 送った回数（送り始める前に増やして保存する。再起動をまたいで上限を共有する）

	sending bool // 送信中（保存しない。二重に始めないための印）
}

// watchEntry は、受理を確かめられなかった指示（send_text・dispatch）の見張りの 1 ペイン分（Issue #118。P-108）。
// サーバーの再起動で見張りが消えないよう、台帳に持つ。見張りの中身は pane.Unaccepted が持ち、これはその写し。
type watchEntry struct {
	Enter    time.Time `json:"enter"`              // 受理を確かめられなかった Enter の時刻
	Retained uint64    `json:"retained,omitempty"` // 0 でなければ、件を送ったあとも見続ける対象で、その印を付けた順
	PID      int       `json:"pid,omitempty"`      // 見張ったエージェントの pid。別のプロセスになっていたら捨てる
}

func watchOf(w pane.Watch) watchEntry {
	return watchEntry{Enter: w.Enter, Retained: w.Retained, PID: w.PID}
}

func (w watchEntry) same(o pane.Watch) bool { return w == watchOf(o) }

func (w watchEntry) toPane() pane.Watch {
	return pane.Watch{Enter: w.Enter, Retained: w.Retained, PID: w.PID}
}

// ledgerVersion は台帳の版。省略できる任意のキー（asked・report_sent・watch）を足すときは上げない（古い台帳もそのまま読める。ER §9）。
const ledgerVersion = 2

// ledgerFile は webhook-state.json。
type ledgerFile struct {
	Version     int               `json:"version"`
	ServerStart string            `json:"server_start,omitempty"` // tmux サーバーの起動時刻。変わったらペイン ID が使い回されるので回を捨てる
	Panes       map[string]*entry `json:"panes"`
	// Asked は、いま asking の回で pane.asking の件を作った（送り先が無くて作らなかった回も含む）ペイン ID → event_id。
	// asking でない状態を見たら消える。再起動しても、同じ質問のまま二度目を作らない。
	Asked map[string]string `json:"asked,omitempty"`
	// Watch は、受理を確かめられなかった指示の見張り（ペイン ID → 記録）。再起動のあとも、長い作業の完了を見逃さないため（Issue #118）。無ければ省略。
	Watch  map[string]watchEntry `json:"watch,omitempty"`
	Outbox map[string]*item      `json:"outbox"`
}

func emptyLedger() ledgerFile {
	return ledgerFile{Version: ledgerVersion, Panes: map[string]*entry{}, Asked: map[string]string{}, Watch: map[string]watchEntry{}, Outbox: map[string]*item{}}
}

// loadLedger は台帳を読む。無ければ空。壊れている・版が違えば空にして、そのことを返す（送信は止めない）。
func loadLedger(path string) (ledgerFile, error) {
	data, err := os.ReadFile(path) //nolint:gosec // config が決めた台帳のパス
	if errors.Is(err, fs.ErrNotExist) {
		return emptyLedger(), nil
	}
	if err != nil {
		return emptyLedger(), err
	}
	var f ledgerFile
	if err := json.Unmarshal(data, &f); err != nil || f.Version != ledgerVersion {
		return emptyLedger(), errors.New("台帳の形が不正です")
	}
	if f.Panes == nil {
		f.Panes = map[string]*entry{}
	}
	if f.Asked == nil {
		f.Asked = map[string]string{}
	}
	for id, ev := range f.Asked {
		if ev == "" {
			delete(f.Asked, id)
		}
	}
	for id, w := range f.Watch {
		if w.Enter.IsZero() || id == "" {
			delete(f.Watch, id)
		}
	}
	if f.Watch == nil {
		f.Watch = map[string]watchEntry{}
	}
	if f.Outbox == nil {
		f.Outbox = map[string]*item{}
	}
	for id, e := range f.Panes {
		if e == nil {
			delete(f.Panes, id)
		}
	}
	for id, it := range f.Outbox {
		if it == nil || it.EventID != id || len(it.Payload) == 0 {
			delete(f.Outbox, id)
		}
	}
	return f, nil
}

func saveLedger(path string, f ledgerFile) error {
	f.Version = ledgerVersion
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return store.WriteFileAtomic(path, data, 0o600)
}
