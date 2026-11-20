// Package agent はエージェント（omp・Claude Code・Codex）ごとの画面解釈・キー操作の契約とレジストリを定める。
// 具体的なアダプタは agent/omp・agent/claude・agent/codex が init() で Register する（FR-AD-4）。
package agent

import (
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"
)

// State はペインの状態（機能要件 §3.1）。
type State string

// 状態の一覧。shell・stalled・unknown はアダプタではなく pane パッケージが決める。
const (
	StateWorking State = "working"
	StateAsking  State = "asking"
	StateIdle    State = "idle"
	StateShell   State = "shell"
	StateStalled State = "stalled"
	StateUnknown State = "unknown"
)

// Screen は capture-pane -p の出力（ANSI なし）を行に分けたもの。
type Screen struct {
	Lines  []string // 画面の最後から最大 80 行
	Width  int
	Height int
}

// Detection は Detect の結果。State は Working・Asking・Idle・Unknown のいずれか。
type Detection struct {
	State    State
	Activity string // いまの 1 行（なければ ""）
	Elapsed  string // 画面の経過時間表示（なければ ""）
}

// Option は質問カードの選択肢。
type Option struct {
	N           int // 1 始まり
	Label       string
	Recommended bool
}

// Card は質問カード。
type Card struct {
	Question     string
	Options      []Option
	Cursor       int // 現在のカーソル番号（1 始まり、不明なら 0）
	AllowsCustom bool
	// Truncated は pane が上限（選択肢 20・質問 1000 字・ラベル 300 字）で切ったとき true にする。アダプタは設定しない。
	Truncated bool
}

// Key は send-keys に渡す 1 キー。Name は tmux のキー名（"Up"・"Down"・"Enter"）か数字 "1".."9"。
type Key struct {
	Name string
}

// Adapter はエージェントごとの違いを閉じ込める（コンポーネント設計 §4）。
type Adapter interface {
	Name() string
	StartCommand() string
	ExitCommand() string
	MatchProcess(argv []string) bool
	MatchScreen(s Screen) bool
	Detect(s Screen) Detection
	ParseCard(s Screen) (Card, bool)
	PickKeys(c Card, n int) ([]Key, error)
	CustomKeys(c Card) ([]Key, error) // 自由入力を選べないカードは apperr.CustomNotAllowed を返す
	Clean(lines []string) []string
	StableText(lines []string) string
}

// PasteMenu は、大きな貼り付けの直後に確認メニューを開くエージェントのアダプタが、Adapter に加えて実装する（omp。Issue #99）。
// メニューが開いている間に送った Enter はメニューの選択になり、依頼の送信にならない。
type PasteMenu interface {
	// PasteMenuOpen は画面が貼り付けの確認メニューを出しているか。
	PasteMenuOpen(s Screen) bool
	// PasteMenuClose はメニューを閉じるキー。貼った本文を 1 文字も変えずに入力欄へ載せる選択肢（omp は Esc の「Paste inline」）にする。
	PasteMenuClose() Key
}

// Submitter は、選択肢を選んだあとに回答の確定（Submit）の Enter がもう 1 回要る質問フォームを出すエージェントのアダプタが、
// Adapter に加えて実装する（omp v19。Issue #111）。
type Submitter interface {
	// SubmitFocused は質問カードのカーソルが Submit の行にあるか。ここで Enter を送ると回答が確定する。
	SubmitFocused(s Screen) bool
}

// WorkProbe は、画面が作業中を映さなくなっても、エージェント自身が書くログから作業の跡を読めるアダプタが、Adapter に加えて実装する（omp。Issue #113）。
// omp は負荷が高いと画面の描き直しが止まり、作業中でも起動画面のまま idle と読めることがある。
type WorkProbe interface {
	// LastWork は pid のプロセスのログで、since 以降にあった作業（モデルの呼び出し）の最後の時刻。
	// 起動時の取得や定期の更新など、作業ではない行は数えない。ログが無い・読めない・作業の行が無いときは ok=false。
	LastWork(pid int, since time.Time) (at time.Time, ok bool)
}

// TurnProbe は、エージェントが会話の記録（omp はセッションの jsonl）をファイルに書くアダプタが、WorkProbe に加えて実装する（omp。Issue #118）。
// 長いコマンド（学習・CI 待ちなど）の実行中はモデルの呼び出しの行がログに出ないので、記録の末尾が「作業の番の途中」かで作業中かを確かめる。
type TurnProbe interface {
	// Turn は pid のプロセスの会話の記録の、since 以降の最後のやり取りの時刻 last と、その番がまだ終わっていないか inTurn を返す。
	// 番の途中は、ユーザーの指示・ツールの呼び出し（結果を待っている）・ツールの結果（次のモデルの応答を待っている）。最終の応答（ツールの呼び出しを伴わない）や中断で終わっていれば inTurn=false。
	// 記録が無い・読めない・since 以降のやり取りが無いときは ok=false。
	Turn(pid int, since time.Time) (last time.Time, inTurn, ok bool)
}

// Toggler は、複数選択（チェックボックス）の質問を出すエージェントのアダプタが、Adapter に加えて実装する（omp。Issue #111）。
// 複数選択では、カーソルを選択肢に合わせて Enter を送っても、その選択肢にはチェックが付かないことがあるので、先にチェックを付ける。
type Toggler interface {
	// ToggleKeys はカーソルが n 番にあるとき、n 番にチェックを付けるキー。すでにチェックされている・単一選択のカードは空。
	ToggleKeys(s Screen, n int) []Key
}

// Overrides は config.toml の [agents.<name>] による上書き。nil / 空は「上書きしない」。
// 正規表現は Go の regexp 構文で、起動時にコンパイルできること。
type Overrides struct {
	Command      *string  `toml:"command"`
	ExitCommand  *string  `toml:"exit_command"`
	ProcessMatch []string `toml:"process_match"`
	WorkingRE    *string  `toml:"working_re"`
	AskRE        *string  `toml:"ask_re"`
	IdleRE       *string  `toml:"idle_re"`
	SpinnerChars *string  `toml:"spinner_chars"`
	StatusLineRE *string  `toml:"status_line_re"`
	BorderRE     *string  `toml:"border_re"`
	AuthFile     *string  `toml:"auth_file"`
}

// Factory は上書きを適用したアダプタを作る。正規表現が不正ならエラー。
type Factory func(ov Overrides) (Adapter, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register はアダプタの Factory を登録する。init() から呼ぶ。重複は panic。
func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := factories[name]; dup {
		panic("agent: duplicate adapter " + name)
	}
	factories[name] = f
}

// New は name のアダプタを ov を適用して作る。
func New(name string, ov Overrides) (Adapter, error) {
	mu.RLock()
	f, ok := factories[name]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("agent: 未登録のアダプタ %q", name)
	}
	return f(ov)
}

// Names は登録済みのアダプタ名を昇順で返す。
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(factories))
	for n := range factories {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// builtinNames は組み込みのエージェント（FR-SAFE-3）。並びはここだけに書く。
// pane_start・dispatch の agent の選択肢、agent_auth_status・report の auth、setup の質問、[agents.<name>] の検査はこの順・この対象。
var builtinNames = [...]string{"omp", "claude", "codex"}

// Builtin は組み込みのエージェント名を決まった並び（omp・claude・codex）で返す。呼ぶたびに新しいスライスを返すので、変更しても元は変わらない。
func Builtin() []string { return slices.Clone(builtinNames[:]) }

// IsBuiltin は name が組み込みのエージェントか。
func IsBuiltin(name string) bool { return slices.Contains(builtinNames[:], name) }
