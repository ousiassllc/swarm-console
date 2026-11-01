package config

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/setup/template"
)

// uncommentedTemplate はひな形のコメント化された設定行（`# key = value`・`# [table]`）の # を外し、
// 必須の listen_addr を使える値にしたもの。説明の文章の行はそのまま（コメントのまま）残す。
func uncommentedTemplate(t *testing.T) string {
	t.Helper()
	setting := regexp.MustCompile(`^# ((?:[A-Za-z0-9_."-]+ = (?:"[^"]*"|\[[^\]]*\]|true|false|[0-9]+)|\[[a-z_.]+\])\s*(?:#.*)?)$`)
	var out []string
	for _, line := range strings.Split(string(template.Bytes()), "\n") {
		if m := setting.FindStringSubmatch(line); m != nil {
			line = m[1]
		}
		out = append(out, line)
	}
	return strings.ReplaceAll(strings.Join(out, "\n"), `"100.x.y.z:48120"`, `"127.0.0.1:48120"`)
}

// ひな形は全キーの説明と既定値を並べる（ER §5.1）。ひな形の値は、書かなかったときの既定と同じでなければならない。
func TestTemplateValuesMatchDefaults(t *testing.T) {
	want, err := ParseBytes([]byte("listen_addr = \"127.0.0.1:48120\"\n"), "config.toml")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseBytes([]byte(uncommentedTemplate(t)), "config.toml")
	if err != nil {
		t.Fatalf("コメントを外したひな形を読めません: %v", err)
	}
	// ひな形の例（[pane_names]・[agents.*]）は既定ではなく見本なので比べない。
	got.PaneNames, want.PaneNames = nil, nil
	got.Agents, want.Agents = nil, nil
	// 書かない tmux_socket は tmux の既定のソケット（名前は default）。ひな形は同じ意味を名前で書いている。
	got.TmuxSocket, want.TmuxSocket = "", ""
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ひな形の値が既定と違います:\n got=%+v\nwant=%+v", got, want)
	}
}

// config が受け付けるトップレベルのキーは、すべてひな形に載っている（キーを足したときの載せ忘れを防ぐ）。
func TestTemplateListsEveryKey(t *testing.T) {
	src := uncommentedTemplate(t)
	rt := reflect.TypeOf(rawConfig{})
	for i := range rt.NumField() {
		key, _, _ := strings.Cut(rt.Field(i).Tag.Get("toml"), ",")
		if key == "" || key == "-" {
			continue
		}
		if !strings.Contains(src, "\n"+key+" = ") && !strings.Contains(src, "\n["+key) {
			t.Errorf("ひな形に %q がありません", key)
		}
	}
}
