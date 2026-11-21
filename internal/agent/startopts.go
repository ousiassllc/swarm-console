package agent

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// MaxModelRunes は model の最大字数（Unicode 字数）。
const MaxModelRunes = 100

// modelRE は model に許す文字。英数字と . _ : / @ - だけ。空白・引用符・改行・制御文字・シェルの特殊文字は入らない。
// `@` は omp のセレクタ（`@slow` など）のため。
var modelRE = regexp.MustCompile(`^[A-Za-z0-9._:/@-]+$`)

// thinkingValues は thinking に許す値（omp の `--thinking`）。完全一致（小文字）だけ。
var thinkingValues = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max", "auto"}

// claudeEfforts は claude の `--effort` に渡せる値。thinkingValues のうち、公式に対応する値があるものだけ。
var claudeEfforts = []string{"low", "medium", "high", "xhigh", "max"}

// StartOptions は起動時に指定する model と thinking。空文字は「指定なし」（旗を付けない）。
// 値は NewStartOptions か Validate を通ったものだけが起動行に入る。
type StartOptions struct {
	Model    string
	Thinking string
}

// IsZero はどちらも指定されていないか。
func (o StartOptions) IsZero() bool { return o.Model == "" && o.Thinking == "" }

// ValidModel は model が文字種と長さの条件に合うか。空文字は合わない。
func ValidModel(s string) bool {
	return utf8.RuneCountInString(s) <= MaxModelRunes && modelRE.MatchString(s)
}

// ValidThinking は thinking が許可リストのどれかと完全一致するか。
func ValidThinking(s string) bool { return slices.Contains(thinkingValues, s) }

// NewStartOptions は API 引数（nil は省略・null）から検証済みの StartOptions を作る。
// 検査の順は model の値 → thinking の値 → エージェントごとの可否。
// 空文字は省略ではなく不正（invalid_model・invalid_thinking）。
func NewStartOptions(agentName string, model, thinking *string) (StartOptions, error) {
	var o StartOptions
	if model != nil {
		o.Model = *model
		if !ValidModel(o.Model) {
			return StartOptions{}, apperr.New(apperr.InvalidModel, "field", "model")
		}
	}
	if thinking != nil {
		o.Thinking = *thinking
		if !ValidThinking(o.Thinking) {
			return StartOptions{}, apperr.New(apperr.InvalidThinking, "field", "thinking")
		}
	}
	if err := o.Validate(agentName); err != nil {
		return StartOptions{}, err
	}
	return o, nil
}

// Validate は値とエージェントとの組み合わせを検査する。何も指定していなければ nil。
func (o StartOptions) Validate(agentName string) error {
	if o.Model != "" && !ValidModel(o.Model) {
		return apperr.New(apperr.InvalidModel, "field", "model")
	}
	if o.Thinking == "" {
		return nil
	}
	if !ValidThinking(o.Thinking) {
		return apperr.New(apperr.InvalidThinking, "field", "thinking")
	}
	switch agentName {
	case "omp":
		return nil
	case "claude":
		if !slices.Contains(claudeEfforts, o.Thinking) {
			return apperr.New(apperr.InvalidThinking, "field", "thinking", "agent", agentName)
		}
		return nil
	case "codex":
		return apperr.New(apperr.ThinkingNA, "field", "thinking", "agent", agentName)
	}
	return apperr.New(apperr.InvalidArgs, "field", "agent")
}

// Command は base（`[agents.<name>] command` 上書き後の起動コマンド）の末尾に、指定した旗だけを足した 1 行を返す。
// 足すのは ` --model=<値>`、omp の ` --thinking=<値>`、claude の ` --effort=<値>` だけで、引用符もほかの旗も付けない。
// 値は検査済みの文字種だけなので、シェルがそのまま 1 語として読む。検査に通らない値はエラー（行を作らない）。
func (o StartOptions) Command(agentName, base string) (string, error) {
	if err := o.Validate(agentName); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(base)
	if o.Model != "" {
		b.WriteString(" --model=" + o.Model)
	}
	if o.Thinking != "" {
		flag := "--thinking="
		if agentName == "claude" {
			flag = "--effort="
		}
		b.WriteString(" " + flag + o.Thinking)
	}
	return b.String(), nil
}
