package setup

import (
	"github.com/pelletier/go-toml/v2"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// keyState は 1 つのキーが config.toml にどう書かれているか。
type keyState struct {
	Present bool // 書かれている（dotted key・インラインテーブルの中も含む）
	Bad     bool // 書かれているが型が違う（値は使えない）
}

// current は config.toml に書かれている値（検証せずに読んだもの）。
// serve の読み込みで落ちる値（listen_addr 未設定を含む）も、画面の初期値として取り出すために使う（FR-SET-8）。
type current struct {
	Values Values // 書かれていないキーは既定値
	Keys   map[string]keyState
}

// 画面で聞くキー（表を含む完全なキーを "." でつないだもの）。
const (
	keyListen  = "listen_addr"
	keySess    = "sessions"
	keyDefault = "default_session"
	keyAllowed = "allowed_roots"
	keyDenied  = "denied_paths"
)

func agentKey(name, field string) string { return "agents." + name + "." + field }

// readCurrent は data（構文の通った TOML）から、画面で聞くキーの現在値を取り出す。型違いはそのキーだけ Bad にする。
func readCurrent(data []byte) current {
	c := current{Values: Defaults(), Keys: map[string]keyState{}}
	var doc map[string]any
	if toml.Unmarshal(data, &doc) != nil {
		return c
	}
	if s, st := str(doc, keyListen); st.Present {
		c.Values.ListenAddr = s
		c.Keys[keyListen] = st
	}
	c.list(doc, keySess, &c.Values.Sessions)
	if s, st := str(doc, keyDefault); st.Present {
		c.Values.DefaultSession = s
		c.Keys[keyDefault] = st
	}
	c.list(doc, keyAllowed, &c.Values.AllowedRoots)
	c.list(doc, keyDenied, &c.Values.DeniedPaths)
	agents, _ := doc["agents"].(map[string]any)
	for _, name := range agent.Builtin() {
		m, _ := agents[name].(map[string]any)
		var a Agent
		for field, dst := range map[string]*string{"command": &a.Command, "exit_command": &a.ExitCommand, "auth_file": &a.AuthFile} {
			if s, st := str(m, field); st.Present {
				*dst = s
				c.Keys[agentKey(name, field)] = st
			}
		}
		c.Values.Agents[name] = a
	}
	return c
}

func str(m map[string]any, key string) (string, keyState) {
	v, ok := m[key]
	if !ok {
		return "", keyState{}
	}
	s, isStr := v.(string)
	return s, keyState{Present: true, Bad: !isStr}
}

func (c *current) list(m map[string]any, key string, dst *[]string) {
	v, ok := m[key]
	if !ok {
		return
	}
	st := keyState{Present: true}
	arr, isArr := v.([]any)
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		s, isStr := e.(string)
		if !isStr {
			isArr = false
			break
		}
		out = append(out, s)
	}
	if !isArr {
		st.Bad = true
		out = nil
	}
	*dst = out
	c.Keys[key] = st
}
