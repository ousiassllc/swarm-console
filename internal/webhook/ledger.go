package webhook

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"

	"github.com/ousiassllc/swarm-console/internal/store"
)

// ペインごとの回（ラウンド）の段階。
const (
	phaseWorking = "working" // working を見た。Base が、そのとき見た最終報告ファイル
	phaseWaiting = "waiting" // working から idle になった。最終報告が Base から変わるのを待つ（まだ送っていない）
	phaseSent    = "sent"    // 送った。この回は二度と送らない
	phaseFailed  = "failed"  // 再送の上限まで試して送れなかった。この回は二度と送らない（ログに残してある）
)

// reportStat は最終報告ファイルの様子。書かれた・更新されたかは、working を見たときの値と変わったかで決める。
type reportStat struct {
	Exists bool  `json:"exists"`
	ModNS  int64 `json:"mtime_ns,omitempty"`
	Size   int64 `json:"size,omitempty"`
}

// entry はペイン 1 つの現在の回。台帳（webhook-state.json）に書き、再起動しても同じ回を続けて二重に送らない。
type entry struct {
	Phase     string     `json:"phase"`
	EventID   string     `json:"event_id,omitempty"`
	ChangedAt time.Time  `json:"state_changed_at"`
	Base      reportStat `json:"report_base"`

	sending bool // 送信中（保存しない。再起動したら送り直す）
}

const ledgerVersion = 1

type ledgerFile struct {
	Version int               `json:"version"`
	Panes   map[string]*entry `json:"panes"`
}

// loadLedger は台帳を読む。無ければ空。壊れていれば空にして、壊れていたことを返す（送信は止めない）。
func loadLedger(path string) (map[string]*entry, error) {
	data, err := os.ReadFile(path) //nolint:gosec // config が決めた台帳のパス
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]*entry{}, nil
	}
	if err != nil {
		return map[string]*entry{}, err
	}
	var f ledgerFile
	if err := json.Unmarshal(data, &f); err != nil || f.Version != ledgerVersion || f.Panes == nil {
		return map[string]*entry{}, errors.New("台帳の形が不正です")
	}
	for id, e := range f.Panes {
		if e == nil {
			delete(f.Panes, id)
		}
	}
	return f.Panes, nil
}

func saveLedger(path string, panes map[string]*entry) error {
	data, err := json.Marshal(ledgerFile{Version: ledgerVersion, Panes: panes})
	if err != nil {
		return err
	}
	return store.WriteFileAtomic(path, data, 0o600)
}
