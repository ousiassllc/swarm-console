package harness

import (
	"fmt"
	"sort"
	"strings"
)

func (h *H) writeConfig(o Options) {
	kv := map[string]string{
		"listen_addr":        fmt.Sprintf("%q", h.Addr),
		"tmux_socket":        fmt.Sprintf("%q", h.TM.Socket),
		"sessions":           fmt.Sprintf("[%q]", Session),
		"allowed_roots":      fmt.Sprintf("[%q]", h.Work),
		"file_roots":         fmt.Sprintf("[%q]", h.Files),
		"report_dir":         fmt.Sprintf("%q", h.Reports),
		"disk_mounts":        `["/"]`,
		"poll_interval":      `"1s"`,
		"snapshot_ttl":       `"200ms"`,
		"accept_timeout":     `"8s"`,
		"start_timeout":      `"30s"`,
		"lock_timeout":       `"30s"`,
		"layout_warning_ttl": `"10m"`,
	}
	for k, v := range o.Config {
		kv[k] = v
	}
	agents := map[string]map[string]string{}
	for _, name := range []string{"omp", "claude", "codex"} {
		// 起動コマンド。--log のパスはシェルが $TMUX_PANE（%N）で展開する。
		agents[name] = map[string]string{"command": fmt.Sprintf(`'%s idle --log "%s/$TMUX_PANE.log"'`, name, h.Logs)}
	}
	for name, m := range o.Agents {
		if agents[name] == nil {
			agents[name] = map[string]string{}
		}
		for k, v := range m {
			agents[name][k] = v
		}
	}
	var b strings.Builder
	for _, k := range sortedKeys(kv) {
		if kv[k] != "" { // 空の値は「そのキーを書かない」（未指定の検証用）
			fmt.Fprintf(&b, "%s = %s\n", k, kv[k])
		}
	}
	for _, name := range sortedKeys(agents) {
		fmt.Fprintf(&b, "\n[agents.%s]\n", name)
		for _, k := range sortedKeys(agents[name]) {
			fmt.Fprintf(&b, "%s = %s\n", k, agents[name][k])
		}
	}
	h.WriteFile(h.Config, b.String())
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
