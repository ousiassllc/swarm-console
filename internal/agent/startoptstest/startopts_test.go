// Package startoptstest は agent.StartOptions（起動時の model・thinking）の検査と起動行の組み立てを確かめる。
package startoptstest

import (
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
)

func ptr(s string) *string { return &s }

func codeOf(err error) apperr.Code {
	var e *apperr.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// 起動行は、起動コマンド（上書き後）の末尾に、指定した旗だけを 1 つずつ足したもの。
func TestCommandLines(t *testing.T) {
	cases := []struct {
		name, agent, base string
		model, thinking   *string
		want              string
	}{
		{"omp 指定なし", "omp", "omp", nil, nil, "omp"},
		{"omp model だけ", "omp", "omp", ptr("anthropic/claude-opus-5-5"), nil, "omp --model=anthropic/claude-opus-5-5"},
		{"omp thinking だけ", "omp", "omp", nil, ptr("high"), "omp --thinking=high"},
		{"omp 両方", "omp", "omp", ptr("anthropic/claude-opus-5-5"), ptr("high"), "omp --model=anthropic/claude-opus-5-5 --thinking=high"},
		{"omp @セレクタ", "omp", "omp", ptr("@slow"), ptr("auto"), "omp --model=@slow --thinking=auto"},
		{"claude model", "claude", "claude", ptr("opus"), nil, "claude --model=opus"},
		{"claude effort", "claude", "claude", nil, ptr("high"), "claude --effort=high"},
		{"claude 両方", "claude", "claude", ptr("claude-opus-5-5"), ptr("xhigh"), "claude --model=claude-opus-5-5 --effort=xhigh"},
		{"claude max", "claude", "claude", nil, ptr("max"), "claude --effort=max"},
		{"codex model", "codex", "codex", ptr("gpt-5.4"), nil, "codex --model=gpt-5.4"},
		// 上書きした command がすでに --model を含んでいても、末尾に足すだけ（二重はクライアントの責任）。
		{"上書きの command に --model", "omp", "omp --model=a", ptr("b"), nil, "omp --model=a --model=b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o, err := agent.NewStartOptions(c.agent, c.model, c.thinking)
			if err != nil {
				t.Fatal(err)
			}
			got, err := o.Command(c.agent, c.base)
			if err != nil || got != c.want {
				t.Fatalf("Command = %q, %v; want %q", got, err, c.want)
			}
			if o.IsZero() != (c.model == nil && c.thinking == nil) {
				t.Errorf("IsZero = %v", o.IsZero())
			}
		})
	}
}

func TestRejections(t *testing.T) {
	long := strings.Repeat("a", agent.MaxModelRunes+1)
	cases := []struct {
		name, agent     string
		model, thinking *string
		want            apperr.Code
	}{
		{"model が空", "omp", ptr(""), nil, apperr.InvalidModel},
		{"model が 101 字", "omp", ptr(long), nil, apperr.InvalidModel},
		{"model に空白", "omp", ptr("a b"), nil, apperr.InvalidModel},
		{"model に改行", "omp", ptr("a\nb"), nil, apperr.InvalidModel},
		{"model に引用符", "omp", ptr(`a"b`), nil, apperr.InvalidModel},
		{"model に全角", "omp", ptr("モデル"), nil, apperr.InvalidModel},
		{"thinking が空", "omp", nil, ptr(""), apperr.InvalidThinking},
		{"thinking が大文字", "omp", nil, ptr("High"), apperr.InvalidThinking},
		{"thinking が未知", "omp", nil, ptr("ultracode"), apperr.InvalidThinking},
		{"thinking に空白", "omp", nil, ptr("high "), apperr.InvalidThinking},
		{"claude で off", "claude", nil, ptr("off"), apperr.InvalidThinking},
		{"claude で minimal", "claude", nil, ptr("minimal"), apperr.InvalidThinking},
		{"claude で auto", "claude", nil, ptr("auto"), apperr.InvalidThinking},
		{"codex で thinking", "codex", nil, ptr("high"), apperr.ThinkingNA},
		{"codex で off でも", "codex", nil, ptr("off"), apperr.ThinkingNA},
		// 値の不正は、エージェントごとの可否より先に出る。
		{"codex で未知の thinking", "codex", nil, ptr("bogus"), apperr.InvalidThinking},
		{"model と thinking の両方が不正なら model", "omp", ptr(""), ptr(""), apperr.InvalidModel},
	}
	for _, c := range cases {
		_, err := agent.NewStartOptions(c.agent, c.model, c.thinking)
		if got := codeOf(err); got != c.want {
			t.Errorf("%s: code = %q, want %q (%v)", c.name, got, c.want, err)
		}
	}
	// ちょうど 100 字は通る。
	if _, err := agent.NewStartOptions("omp", ptr(strings.Repeat("a", agent.MaxModelRunes)), nil); err != nil {
		t.Errorf("100 字は通るはず: %v", err)
	}
}

// 検査済みの値だけが起動行に入る。検査を通らない StartOptions は、直接 Command に渡しても行を作らない（二重防御）。
func TestCommandRevalidates(t *testing.T) {
	for _, o := range []agent.StartOptions{
		{Model: "x; touch /tmp/pwned"},
		{Model: "$(id)"},
		{Thinking: "high; id"},
		{Thinking: "off"}, // claude では拒否
	} {
		if got, err := o.Command("claude", "claude"); err == nil {
			t.Errorf("%+v: 行 %q を作ってしまった", o, got)
		}
	}
}

// 許す文字は、どれもシェルが特別に扱わない（引用符なしで 1 語になる）。注入に使える文字は 1 つも通らない。
func TestNoShellMetacharacterGetsThrough(t *testing.T) {
	const special = " \t\n\r\"'`$&|;<>()[]{}*?!#~\\=%^,+"
	for _, r := range special {
		for _, model := range []string{string(r), "a" + string(r) + "b", "a" + string(r), string(r) + "a"} {
			if agent.ValidModel(model) {
				t.Errorf("model %q が通った", model)
			}
		}
	}
	// 既知の注入の形。
	for _, model := range []string{
		"x; rm -rf /", "x && id", "x || id", "x | tee /tmp/a", "x > /tmp/a", "$(id)", "`id`", "${IFS}id", "x\nid", "x\rid",
		"x'y", `x"y`, "x\\y", "~/a", "a b", "-- --dangerously-skip-permissions", "x # c",
	} {
		if _, err := agent.NewStartOptions("omp", &model, nil); codeOf(err) != apperr.InvalidModel {
			t.Errorf("model %q: err = %v", model, err)
		}
	}
	// 許す文字だけで作った値は、起動行に 1 語（=<値> の形）としてそのまま入る。
	const allowed = "azAZ09._:/@-"
	o, err := agent.NewStartOptions("omp", ptr(allowed), nil)
	if err != nil {
		t.Fatal(err)
	}
	line, err := o.Command("omp", "omp")
	if err != nil || line != "omp --model="+allowed {
		t.Fatalf("line = %q, %v", line, err)
	}
	if fields := strings.Fields(line); len(fields) != 2 {
		t.Errorf("語が %d 個に分かれた: %v", len(fields), fields)
	}
}
