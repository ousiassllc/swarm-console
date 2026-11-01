package config

import (
	"fmt"
	"regexp"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// checkAgents は [agents.<name>] の上書きを検査する。正規表現は構文だけを確かめる
// （コンパイル結果は agent.New が作る）。command・exit_command は端末に打つので 1 行・制御文字なし。
func checkAgents(in map[string]agent.Overrides, home string) (map[string]agent.Overrides, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]agent.Overrides, len(in))
	for name, ov := range in {
		if !agent.IsBuiltin(name) {
			return nil, fmt.Errorf("[agents.%s] は未対応です（omp・claude・codex）", name)
		}
		if err := checkOverrides(name, &ov, home); err != nil {
			return nil, err
		}
		out[name] = ov
	}
	return out, nil
}

func checkOverrides(name string, ov *agent.Overrides, home string) error {
	for key, p := range map[string]*string{"command": ov.Command, "exit_command": ov.ExitCommand} {
		if p == nil {
			continue
		}
		if *p == "" {
			return fmt.Errorf("agents.%s.%s は空にできません", name, key)
		}
		if err := CheckPlainLine(fmt.Sprintf("agents.%s.%s", name, key), *p); err != nil {
			return err
		}
	}
	res := map[string]*string{
		"working_re": ov.WorkingRE, "ask_re": ov.AskRE, "idle_re": ov.IdleRE,
		"status_line_re": ov.StatusLineRE, "border_re": ov.BorderRE,
	}
	for key, p := range res {
		if p == nil {
			continue
		}
		if _, err := regexp.Compile(*p); err != nil {
			return fmt.Errorf("agents.%s.%s の正規表現が不正です: %w", name, key, err)
		}
	}
	for i, p := range ov.ProcessMatch {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("agents.%s.process_match[%d] の正規表現が不正です: %w", name, i, err)
		}
	}
	if ov.AuthFile != nil {
		p, err := ExpandPath(fmt.Sprintf("agents.%s.auth_file", name), *ov.AuthFile, home)
		if err != nil {
			return err
		}
		ov.AuthFile = &p
	}
	return nil
}
