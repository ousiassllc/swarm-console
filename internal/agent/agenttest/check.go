package agenttest

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// Row はサンプル画面 1 枚の期待結果（状態・活動・経過・カード）。Card が nil なら ParseCard は ok=false になること。
type Row struct {
	File     string
	State    agent.State
	Activity string
	Elapsed  string
	Card     *agent.Card
}

// KeyRow は PickKeys（N>0）または CustomKeys（N==0）の期待結果。ErrCode が空でなければそのエラーになること。
type KeyRow struct {
	File    string
	N       int
	Want    []string
	ErrCode apperr.Code
}

// CheckRows は dir（testdata/screens/<name>）の全サンプルが rows に載っていて、期待どおり判定されることを確かめる。
func CheckRows(t *testing.T, a agent.Adapter, name string, rows []Row) {
	t.Helper()
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.File] = true
		t.Run(r.File, func(t *testing.T) {
			s := Load(t, name, r.File)
			got := a.Detect(s)
			want := agent.Detection{State: r.State, Activity: r.Activity, Elapsed: r.Elapsed}
			if got != want {
				t.Errorf("Detect = %+v, want %+v", got, want)
			}
			c, ok := a.ParseCard(s)
			if ok != (r.Card != nil) {
				t.Fatalf("ParseCard ok = %v, want %v (card=%+v)", ok, r.Card != nil, c)
			}
			if r.Card != nil && !equalCard(c, *r.Card) {
				t.Errorf("ParseCard = %+v, want %+v", c, *r.Card)
			}
		})
	}
	for _, f := range Files(t, name) {
		if !seen[f] {
			t.Errorf("サンプル %s/%s が期待結果の表にありません", name, f)
		}
	}
}

func equalCard(a, b agent.Card) bool {
	return a.Question == b.Question && a.Cursor == b.Cursor && a.AllowsCustom == b.AllowsCustom && slices.Equal(a.Options, b.Options)
}

// CheckKeys は各サンプルのカードに対する PickKeys・CustomKeys の列を確かめる。
func CheckKeys(t *testing.T, a agent.Adapter, name string, rows []KeyRow) {
	t.Helper()
	for _, r := range rows {
		t.Run(r.File, func(t *testing.T) {
			c, ok := a.ParseCard(Load(t, name, r.File))
			if !ok {
				t.Fatal("カードを読めません")
			}
			var keys []agent.Key
			var err error
			if r.N == 0 {
				keys, err = a.CustomKeys(c)
			} else {
				keys, err = a.PickKeys(c, r.N)
			}
			if r.ErrCode != "" {
				var ae *apperr.Error
				if !errors.As(err, &ae) || ae.Code != r.ErrCode {
					t.Fatalf("err = %v, want code %s", err, r.ErrCode)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, len(keys))
			for i, k := range keys {
				got[i] = k.Name
			}
			if !slices.Equal(got, r.Want) {
				t.Errorf("keys = %v, want %v", got, r.Want)
			}
		})
	}
}

// CheckStable は same の各組が同じ StableText になり、differ の各組が違う StableText になることを確かめる。
func CheckStable(t *testing.T, a agent.Adapter, name string, same, differ [][2]string) {
	t.Helper()
	stable := func(f string) string { return a.StableText(Load(t, name, f).Lines) }
	for _, p := range same {
		if x, y := stable(p[0]), stable(p[1]); x != y || x == "" {
			t.Errorf("StableText(%s) と StableText(%s) が同じになりません:\n%q\n%q", p[0], p[1], x, y)
		}
	}
	for _, p := range differ {
		if stable(p[0]) == stable(p[1]) {
			t.Errorf("StableText(%s) と StableText(%s) は違う本文なのに同じです", p[0], p[1])
		}
	}
}

// CheckClean は Clean の結果を確かめる。
func CheckClean(t *testing.T, a agent.Adapter, name, file string, want []string) {
	t.Helper()
	got := a.Clean(Load(t, name, file).Lines)
	if !slices.Equal(got, want) {
		t.Errorf("Clean(%s) = %q, want %q", file, got, want)
	}
}

// CheckCleanHas は Clean の結果に keep の文字列が全部含まれ、drop の文字列が 1 つも含まれないことを確かめる。
func CheckCleanHas(t *testing.T, a agent.Adapter, name, file string, keep, drop []string) {
	t.Helper()
	got := strings.Join(a.Clean(Load(t, name, file).Lines), "\n")
	for _, k := range keep {
		if !strings.Contains(got, k) {
			t.Errorf("Clean(%s) に %q がありません", file, k)
		}
	}
	for _, d := range drop {
		if strings.Contains(got, d) {
			t.Errorf("Clean(%s) に除くべき %q が残っています", file, d)
		}
	}
}
