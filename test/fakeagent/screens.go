package main

import (
	"embed"
	"fmt"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// screens は testdata/screens の実画面サンプルのうち擬似エージェントが使う最小の組。
// embed は親フォルダを参照できないので、ここに置いた写しが testdata/screens と一致することをテストで確かめる。
//
//go:embed screens
var screens embed.FS

// ready は先頭に常に出す準備完了の行。テストは capture-pane でこの行を待つ。
const ready = "READY"

// sample はサンプル画面を行にして返す（注記行は除く）。
func sample(agentName, file string) ([]string, error) {
	b, err := screens.ReadFile("screens/" + agentName + "/" + file)
	if err != nil {
		return nil, fmt.Errorf("サンプル画面がありません: %w", err)
	}
	return agent.SampleLines(string(b)), nil
}

// frameFile は現在の状態に対応するサンプル画面のファイル名。
func (m *machine) frameFile() string {
	switch m.mode {
	case modeWorking:
		if m.tick%2 == 0 {
			return "working.txt"
		}
		return "working-b.txt"
	case modeAsk:
		return m.askFile(m.cursor)
	case modeCustom:
		if m.agent == "omp" {
			return "custom-input.txt"
		}
		return m.askFile(m.rows.customRow)
	}
	return "idle.txt"
}

func (m *machine) askFile(cursor int) string {
	switch {
	case m.agent == "omp":
		return fmt.Sprintf("ask-%d.txt", cursor)
	case m.agent == "claude" && m.scenario == "ask-custom":
		return fmt.Sprintf("question-%d.txt", cursor)
	case m.agent == "claude":
		return fmt.Sprintf("permission-%d.txt", cursor)
	}
	return fmt.Sprintf("approval-%d.txt", cursor)
}

// frameRows は画面の行数。サンプルがこれだけ埋まっているとき、READY は先頭の行を置き換える
// （足すと画面が 1 行スクロールして READY が消える）。テストの tmux は 40 行で起動する。
const frameRows = 40

// Frame は画面に出す行。先頭の空行（または画面が満杯のときは先頭の行）を READY の行に置き換える。
func (m *machine) Frame() ([]string, error) {
	lines, err := sample(m.agent, m.frameFile())
	if err != nil {
		return nil, err
	}
	lines = append([]string(nil), lines...)
	if m.mode == modeCustom {
		lines = m.withTyped(lines)
	}
	if len(lines) > 0 && (strings.TrimSpace(lines[0]) == "" || len(lines) >= frameRows) {
		lines[0] = ready
		return lines, nil
	}
	return append([]string{ready}, lines...), nil
}

// withTyped は自由入力中の本文を入力欄に出す（omp は `> ` の行、Claude Code は「Type something.」の項）。
func (m *machine) withTyped(lines []string) []string {
	text := string(m.typed)
	for i, l := range lines {
		switch {
		case m.agent == "omp" && strings.HasPrefix(l, "│ >"):
			pad := max(len([]rune(l))-len([]rune("│ > "+text))-1, 0)
			lines[i] = "│ > " + text + strings.Repeat(" ", pad) + "│"
		case m.agent == "claude" && text != "" && strings.Contains(l, "Type something."):
			lines[i] = strings.Replace(l, "Type something.", text, 1)
		}
	}
	return lines
}
