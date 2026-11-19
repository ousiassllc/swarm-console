package webhook

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"

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
}

// item は送る約束をした 1 件（outbox）。回の状態とは独立で、ペインが working に戻っても・消えても・tmux が再起動しても、
// 送り終える（または諦める）まで台帳に残り、本文はそのまま（同じ署名で）送る。
type item struct {
	EventID  string          `json:"event_id"`
	Payload  json.RawMessage `json:"payload"`  // 送る本文そのもの。再送も再起動後も、この 1 バイトも変えずに送る
	Attempts int             `json:"attempts"` // 送った回数（送り始める前に増やして保存する。再起動をまたいで上限を共有する）

	sending bool // 送信中（保存しない。二重に始めないための印）
}

const ledgerVersion = 2

// ledgerFile は webhook-state.json。
type ledgerFile struct {
	Version     int               `json:"version"`
	ServerStart string            `json:"server_start,omitempty"` // tmux サーバーの起動時刻。変わったらペイン ID が使い回されるので回を捨てる
	Panes       map[string]*entry `json:"panes"`
	// Asked は、いま asking の回で pane.asking の件を作った（送り先が無くて作らなかった回も含む）ペイン ID → event_id。
	// asking でない状態を見たら消える。再起動しても、同じ質問のまま二度目を作らない。
	Asked  map[string]string `json:"asked,omitempty"`
	Outbox map[string]*item  `json:"outbox"`
}

func emptyLedger() ledgerFile {
	return ledgerFile{Version: ledgerVersion, Panes: map[string]*entry{}, Asked: map[string]string{}, Outbox: map[string]*item{}}
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
