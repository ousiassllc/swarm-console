package setup

import (
	"slices"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/config/tomledit"
)

// Diff は画面の入力 ans を、現在の config.toml の内容 st に対する編集操作にする（FR-SET-3）。
// 運用者が値を変えたキーだけを返す。値が変わっていないキー・ファイルに無く既定のままのキーは返さない（既定値を書き出さない）。
// 比較は検証・`~` 展開・Clean 後の実効値で行う。順番は聞く順（listen_addr → sessions → … → エージェント）。
func Diff(st *State, ans Values, home string) []tomledit.Op {
	d := differ{cur: st.Cur, home: home}
	d.listen(ans.ListenAddr)
	d.sessions(ans.Sessions, ans.DefaultSession)
	d.paths(keyAllowed, ans.AllowedRoots, config.DefaultAllowedRoots())
	d.paths(keyDenied, ans.DeniedPaths, config.DefaultDeniedPaths())
	for _, name := range AgentNames {
		d.agent(name, ans.Agents[name])
	}
	return d.ops
}

// Keys は ops のキーを "agents.omp.command" の形の文字列にする（保存後の表示用）。
func Keys(ops []tomledit.Op) []string {
	out := make([]string, len(ops))
	for i, o := range ops {
		out[i] = strings.Join(o.Key, ".")
	}
	return out
}

type differ struct {
	cur  current
	home string
	ops  []tomledit.Op
}

func (d *differ) put(key string, v any) {
	d.ops = append(d.ops, tomledit.Op{Key: strings.Split(key, "."), Value: v})
}

func (d *differ) listen(ans string) {
	ans = strings.TrimSpace(ans)
	st := d.cur.Keys[keyListen]
	if ans == "" {
		return // 画面は空を通さない。念のため何も書かない
	}
	if st.Present && !st.Bad {
		if a, err := config.CheckListenAddr(ans); err == nil {
			if c, err := config.CheckListenAddr(d.cur.Values.ListenAddr); err == nil && a == c {
				return
			}
		}
	}
	d.put(keyListen, ans)
}

// sessions は sessions と default_session。default_session は 2 つ以上のときだけ聞くので、
// 1 つのときは、ファイルにある default_session が sessions から外れる（検証で落ちる）場合だけ合わせる。
func (d *differ) sessions(sessions []string, def string) {
	if len(sessions) == 0 {
		return
	}
	curSess := d.cur.Values.Sessions
	if st := d.cur.Keys[keySess]; st.Bad || !slices.Equal(curSess, sessions) {
		d.put(keySess, sessions)
	}
	st := d.cur.Keys[keyDefault]
	curDef := d.cur.Values.DefaultSession
	switch {
	case len(sessions) == 1:
		if st.Present && (st.Bad || curDef != sessions[0]) {
			d.put(keyDefault, sessions[0])
		}
	case st.Present && !st.Bad && curDef == def:
	case !st.Present && def == sessions[0]: // 未設定は先頭と同じ扱い（P-27）
	default:
		d.put(keyDefault, def)
	}
}

func (d *differ) paths(key string, ans, def []string) {
	if ans == nil {
		ans = []string{}
	}
	cur := d.cur.Values
	have, st := cur.AllowedRoots, d.cur.Keys[key]
	if key == keyDenied {
		have = cur.DeniedPaths
	}
	if !st.Present {
		have = def
	}
	if !st.Bad && samePaths(have, ans, d.home) {
		return
	}
	d.put(key, ans)
}

func (d *differ) agent(name string, ans Agent) {
	cur := d.cur.Values.Agents[name]
	def := AgentDefaults(name)
	fields := []struct {
		field        string
		ans, cur, df string
	}{
		{"command", ans.Command, cur.Command, def.Command},
		{"exit_command", ans.ExitCommand, cur.ExitCommand, def.ExitCommand},
		{"auth_file", ans.AuthFile, cur.AuthFile, def.AuthFile},
	}
	for _, f := range fields {
		a := strings.TrimSpace(f.ans)
		key := agentKey(name, f.field)
		st := d.cur.Keys[key]
		switch {
		case a == "": // 空は「上書きしない」。書かれているキーを消すことはしない
		case st.Present && !st.Bad && d.sameField(f.field, a, f.cur):
		case !st.Present && d.sameField(f.field, a, f.df):
		default:
			d.put(key, a)
		}
	}
}

func (d *differ) sameField(field, a, b string) bool {
	if field == "auth_file" {
		return samePaths([]string{a}, []string{b}, d.home)
	}
	return a == b
}
