package auth

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

func TestAuthenticateSuccess(t *testing.T) {
	f := newFixture(t)
	for name, scope := range map[string]Scope{"rep": ScopeReport, "adm": ScopeOps} {
		c, err := f.a.Authenticate(f.bearer(name))
		if err != nil || c.Name != name || c.Scope != scope || c.AllowedSessions != nil {
			t.Fatalf("%s: c=%+v err=%v", name, c, err)
		}
	}
	// スキーム名の大文字小文字は問わない。
	if _, err := f.a.Authenticate("bEaReR " + f.tokens["rep"]); err != nil {
		t.Fatalf("小文字スキーム: %v", err)
	}
}

func TestAuthenticateAllFailuresAreIdenticalUnauthorized(t *testing.T) {
	f := newFixture(t)
	f.add(t, "old", ScopeReport)
	if err := Revoke(f.path, "old", f.clk.Now()); err != nil {
		t.Fatal(err)
	}
	f.open(t)
	tok := f.tokens["rep"]
	cases := map[string]string{
		"ヘッダーなし":     "",
		"Bearer でない": "Basic " + tok,
		"トークン部なし":    "Bearer ",
		"スキームのみ":     "Bearer",
		"短い":         "Bearer sc_x",
		"違うトークン":     "Bearer " + tok + "x",
		"失効済み":       "Bearer " + f.tokens["old"],
		"生トークン":      tok,
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			c, err := f.a.Authenticate(h)
			requireUnauthorized(t, err)
			if c.Name != "" || c.Scope != "" || c.AllowedSessions != nil {
				t.Fatalf("失敗時は空の Client: %+v", c)
			}
		})
	}
}

func TestAllowed(t *testing.T) {
	rep := Client{Name: "rep", Scope: ScopeReport}
	ops := Client{Name: "adm", Scope: ScopeOps}
	if err := Allowed(rep, "/report"); err != nil {
		t.Errorf("report で /report: %v", err)
	}
	if err := Allowed(ops, "/report"); err != nil {
		t.Errorf("ops で /report: %v", err)
	}
	if err := Allowed(ops, "/ops"); err != nil {
		t.Errorf("ops で /ops: %v", err)
	}
	requireCode(t, Allowed(rep, "/ops"), apperr.ForbiddenScope)
	requireCode(t, Allowed(ops, "/other"), apperr.NotFound)
	requireCode(t, Allowed(Client{Name: "x", Scope: "bogus"}, "/report"), apperr.ForbiddenScope)
}

func TestContextRoundTrip(t *testing.T) {
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("空の ctx に Client はない")
	}
	want := Client{Name: "adm", Scope: ScopeOps}
	got, ok := FromContext(WithClient(context.Background(), want))
	if !ok || got.Name != want.Name || got.Scope != want.Scope {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

// 比較は全エントリで必ず行われ、一致位置・失効の有無で回数が変わらない。
func TestAuthenticateScansAllEntries(t *testing.T) {
	f := newFixture(t)
	f.add(t, "third", ScopeReport)
	f.add(t, "gone", ScopeOps)
	if err := Revoke(f.path, "gone", f.clk.Now()); err != nil {
		t.Fatal(err)
	}
	f.open(t)
	const entries = 4
	calls := 0
	f.a.compare = func(a, b []byte) int {
		calls++
		return subtle.ConstantTimeCompare(a, b)
	}
	cases := []struct {
		name    string
		header  string
		wantErr bool
	}{
		{"先頭に一致", f.bearer("rep"), false},
		{"末尾寄りに一致", f.bearer("third"), false},
		{"失効済みに一致", f.bearer("gone"), true},
		{"どれにも不一致", "Bearer sc_nomatch", true},
	}
	for _, c := range cases {
		calls = 0
		_, err := f.a.Authenticate(c.header)
		if (err != nil) != c.wantErr {
			t.Fatalf("%s: err=%v", c.name, err)
		}
		if calls != entries {
			t.Fatalf("%s: 比較 %d 回, want %d", c.name, calls, entries)
		}
	}
}

func TestAuthenticateConcurrentWithRevoke(t *testing.T) {
	f := newFixture(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				if _, err := f.a.Authenticate(f.bearer("rep")); err != nil {
					requireUnauthorized(t, err)
				}
			}
		})
	}
	wg.Go(func() {
		if err := Revoke(f.path, "rep", f.clk.Now()); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	bumpMtime(t, f.path)
	_, err := f.a.Authenticate(f.bearer("rep"))
	requireUnauthorized(t, err)
}

func TestMissingFileAllUnauthorizedThenPicksUpCreatedFile(t *testing.T) {
	f := newFixture(t)
	missing := f.path + ".none"
	a, err := Open(missing, f.clk, nil)
	if err != nil {
		t.Fatalf("ファイル無しで Open は成功する: %v", err)
	}
	_, err = a.Authenticate(f.bearer("adm"))
	requireUnauthorized(t, err)
	// 後からファイルができたら読み込む。
	tok, err := Add(missing, "late", ScopeOps, f.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	if c, err := a.Authenticate("Bearer " + tok); err != nil || c.Name != "late" {
		t.Fatalf("c=%+v err=%v", c, err)
	}
}

func TestReloadAfterRevokeWithoutRestart(t *testing.T) {
	f := newFixture(t)
	if _, err := f.a.Authenticate(f.bearer("rep")); err != nil {
		t.Fatal(err)
	}
	if err := Revoke(f.path, "rep", f.clk.Now()); err != nil {
		t.Fatal(err)
	}
	bumpMtime(t, f.path)
	_, err := f.a.Authenticate(f.bearer("rep"))
	requireUnauthorized(t, err)
	if _, err := f.a.Authenticate(f.bearer("adm")); err != nil {
		t.Fatalf("他のトークンは生きている: %v", err)
	}
}

func TestBadPermissionOrBrokenFileFailsClosedThenRecovers(t *testing.T) {
	f := newFixture(t)
	good := readFile(t, f.path)

	// 権限が 600 でなくなった → 拒否、戻したら復旧。
	chmod(t, f.path, 0o644)
	_, err := f.a.Authenticate(f.bearer("adm"))
	requireUnauthorized(t, err)
	chmod(t, f.path, 0o600)
	if _, err := f.a.Authenticate(f.bearer("adm")); err != nil {
		t.Fatalf("権限を戻したら復旧する: %v", err)
	}

	// 壊れた TOML → 拒否、直したら復旧。
	writeRaw(t, f.path, "[[token]\nname = ", 0o600)
	bumpMtime(t, f.path)
	_, err = f.a.Authenticate(f.bearer("adm"))
	requireUnauthorized(t, err)
	writeRaw(t, f.path, good, 0o600)
	bumpMtime(t, f.path)
	if _, err := f.a.Authenticate(f.bearer("adm")); err != nil {
		t.Fatalf("直したら復旧する: %v", err)
	}
}

// 再読み込みの警告は注入したロガーへ出る（NFR-OPS-5）。nil ならパニックせず出さない。
func TestRefreshWarningUsesInjectedLogger(t *testing.T) {
	f := newFixture(t)
	var buf bytes.Buffer
	a, err := Open(f.path, f.clk, slog.New(slog.NewJSONHandler(&buf, nil)))
	if err != nil {
		t.Fatal(err)
	}
	chmod(t, f.path, 0o644)
	_, err = a.Authenticate(f.bearer("adm"))
	requireUnauthorized(t, err)
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("警告が 1 行の JSON でない: %q: %v", buf.String(), err)
	}
	if rec["level"] != "WARN" || rec["msg"] != "tokens.toml を読み込めないため認証を拒否します" || rec["error"] == nil {
		t.Errorf("警告の中身が違う: %v", rec)
	}

	// nil ロガー（f.a）でも拒否は同じで、パニックしない。
	_, err = f.a.Authenticate(f.bearer("adm"))
	requireUnauthorized(t, err)
}
