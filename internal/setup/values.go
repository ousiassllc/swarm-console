package setup

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/config"
)

// AgentNames は画面で設定を聞くエージェント（FR-SET の項目 5）。
var AgentNames = []string{"omp", "claude", "codex"}

// Agent は 1 エージェントの command・exit_command・auth_file。空は「書かれていない（上書きしない）」。
type Agent struct {
	Command, ExitCommand, AuthFile string
}

// Values は画面で聞く項目の値（聞かないキーは持たない）。文字列・パスは config.toml に書かれた形（`~` 展開前）。
type Values struct {
	ListenAddr     string
	Sessions       []string
	DefaultSession string
	AllowedRoots   []string
	DeniedPaths    []string
	Agents         map[string]Agent
}

// Defaults は config.toml に何も書かなかったときの値（Load と同じ既定）。listen_addr は既定なし。
func Defaults() Values {
	return Values{
		Sessions:     config.DefaultSessions(),
		AllowedRoots: config.DefaultAllowedRoots(),
		DeniedPaths:  config.DefaultDeniedPaths(),
		Agents:       map[string]Agent{},
	}
}

// AgentDefaults は組み込みの既定（入力欄の例示。アダプタが持つ値を使い、ここへコピーしない）。
// auth_file は host_status が見る既定の場所（アダプタを持たないものは空）。
func AgentDefaults(name string) Agent {
	a, err := agent.New(name, agent.Overrides{})
	if err != nil {
		return Agent{}
	}
	d := Agent{Command: a.StartCommand(), ExitCommand: a.ExitCommand()}
	switch name {
	case "claude":
		d.AuthFile = "~/.claude/.credentials.json"
	case "codex":
		d.AuthFile = "~/.codex/auth.json"
	}
	return d
}

// ParseSessions はカンマ区切りの 1 行を sessions にする（前後の空白を除き、空の要素は捨てる）。
func ParseSessions(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParsePaths は 1 行 1 パスの複数行入力をパスの列にする（前後の空白を除き、空行は捨てる）。
func ParsePaths(s string) []string {
	var out []string
	for _, p := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ValidateListen は listen_addr の入力検証。serve の起動時と同じ関数（FR-SET-5）。
func ValidateListen(s string) error {
	_, err := config.CheckListenAddr(strings.TrimSpace(s))
	return err
}

// ValidateSessions は sessions の入力（カンマ区切り）の検証。
func ValidateSessions(s string) error {
	sessions := ParseSessions(s)
	if len(sessions) == 0 { // 空は CheckSessions では既定 ["0"] になるが、画面では消された入力として拒否する
		return errors.New("sessions は 1 つ以上必要です")
	}
	_, _, err := config.CheckSessions(sessions, "")
	return err
}

// ValidatePaths は 1 行 1 パスの入力の検証（絶対パス・`~/` のみ・制御文字なし）。
// 1 つ以上要るときは required を true にする（allowed_roots）。
func ValidatePaths(key, s, home string, required bool) error {
	paths := ParsePaths(s)
	if required && len(paths) == 0 {
		return fmt.Errorf("%s は 1 つ以上必要です", key)
	}
	for _, p := range paths {
		if _, err := config.ExpandPath(key, p, home); err != nil {
			return err
		}
	}
	return nil
}

// ValidateAgentField は command・exit_command・auth_file の入力の検証。空は「上書きしない」で通す。
func ValidateAgentField(name, field, s, home string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	key := "agents." + name + "." + field
	if field == "auth_file" {
		_, err := config.ExpandPath(key, s, home)
		return err
	}
	return config.CheckPlainLine(key, s)
}

// samePaths はパスの列が、`~` 展開・Clean 後の実効値で同じか。展開できないものは文字列そのままで比べる。
func samePaths(a, b []string, home string) bool {
	norm := func(in []string) []string {
		out := make([]string, len(in))
		for i, p := range in {
			if e, err := config.ExpandPath("", p, home); err == nil {
				p = e
			}
			out[i] = p
		}
		return out
	}
	return slices.Equal(norm(a), norm(b))
}
