package notes

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/store"
)

const (
	fileVersion = 1
	maxHistory  = 500
	maxLogLines = 50
	fileMode    = 0o600
)

// SecretDetector は文字列が秘密（P-14 のパターン）に見えるかを返す。
// 本番では safety.DetectSecret を渡す。値そのものをログ・エラーに出してはならない。
type SecretDetector func(string) bool

// Option は NewStore の設定。
type Option func(*fileStore)

// WithSecretDetector は秘密の検出器を設定する。NewStore に必須（無ければエラー。検査なしで動かさない）。
func WithSecretDetector(d SecretDetector) Option {
	return func(s *fileStore) { s.detect = d }
}

// WithLogger は退避の警告を出すロガーを設定する。nil（既定）なら出さない。
func WithLogger(l *slog.Logger) Option {
	return func(s *fileStore) {
		if l != nil {
			s.logger = l
		}
	}
}

// WithWriteFile は書き込み関数を差し替える（書き込み途中停止の注入用。既定は store.WriteFileAtomic）。
func WithWriteFile(w func(path string, data []byte, perm os.FileMode) error) Option {
	return func(s *fileStore) { s.write = w }
}

// fileData は notes.json の中身（ER §2.1）。JSON のキー順は宣言順。
type fileData struct {
	Version         int              `json:"version"`
	TmuxServerStart string           `json:"tmux_server_start"`
	Panes           map[string]*Note `json:"panes"`
	History         []HistoryEntry   `json:"history"`
}

// shallowCopy は更新用の浅いコピーを返す。Note・History は書き換えず作り直す前提。
func (d fileData) shallowCopy() fileData {
	next := d
	next.Panes = make(map[string]*Note, len(d.Panes))
	for k, v := range d.Panes {
		next.Panes[k] = v
	}
	return next
}

type fileStore struct {
	mu     sync.RWMutex
	path   string
	clk    clock.Clock
	detect SecretDetector
	logger *slog.Logger
	write  func(string, []byte, os.FileMode) error
	data   fileData
}

// NewStore は path の notes.json を開く。無ければ空で始める。壊れていれば
// notes.json.corrupt-<時刻> に退避して空で始め、警告をログに出す（NFR-AVL-3）。
func NewStore(path string, clk clock.Clock, opts ...Option) (Store, error) {
	s := &fileStore{path: path, clk: clk, logger: slog.New(slog.DiscardHandler), write: store.WriteFileAtomic}
	for _, o := range opts {
		o(s)
	}
	if s.detect == nil {
		return nil, errors.New("notes: WithSecretDetector が必要です")
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *fileStore) now() time.Time { return s.clk.Now() }

func (s *fileStore) stamp() string { return s.now().Format(time.RFC3339) }

// save は d を書き出す。成功したときだけ呼び出し側が s.data を差し替える。
func (s *fileStore) save(d *fileData) error {
	b, err := marshalData(d)
	if err != nil {
		return apperr.Wrap(apperr.Internal, err)
	}
	if err := s.write(s.path, b, fileMode); err != nil {
		return apperr.Wrap(apperr.Internal, err)
	}
	return nil
}

// errNoChange は fn が「書き込み不要」を伝える目印。update はエラーにせず保存もしない。
var errNoChange = errors.New("no change")

// update は排他の下で d の複製を fn で書き換え、保存に成功したら反映する。失敗したら何も変えない。
func (s *fileStore) update(fn func(d *fileData) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.data.shallowCopy()
	if err := fn(&next); err != nil {
		if errors.Is(err, errNoChange) {
			return nil
		}
		return err
	}
	if err := s.save(&next); err != nil {
		return err
	}
	s.data = next
	return nil
}

func (s *fileStore) load() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		s.data = emptyData()
		return nil
	}
	if err != nil {
		return fmt.Errorf("notes: 読み込み失敗: %w", err)
	}
	d, perr := parseData(raw)
	if perr != nil {
		dest, qerr := s.quarantine()
		if qerr != nil {
			return qerr
		}
		s.logger.Warn("notes.json が壊れているため退避して空で開始します", "path", s.path, "moved_to", dest, "reason", perr.Error())
		s.data = emptyData()
		return nil
	}
	s.data = d
	return nil
}

// quarantine は壊れたファイルを .corrupt-<時刻> へ移す。同名があれば連番を付ける。
func (s *fileStore) quarantine() (string, error) {
	base := s.path + ".corrupt-" + s.now().Format("20060102T150405")
	dest := base
	for i := 1; ; i++ {
		if _, err := os.Lstat(dest); errors.Is(err, fs.ErrNotExist) {
			break
		}
		dest = fmt.Sprintf("%s-%d", base, i)
	}
	if err := os.Rename(s.path, dest); err != nil {
		return "", fmt.Errorf("notes: 壊れたファイルを退避できません: %w", err)
	}
	return dest, nil
}
