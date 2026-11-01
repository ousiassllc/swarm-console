package updatecheck_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/updatecheck"
	"github.com/ousiassllc/swarm-console/internal/updatecheck/updatechecktest"
)

const apiPath = "/repos/ousiassllc/swarm-console/releases/latest"

// release は tag_name を返す releases/latest の応答。assets の URL は使われないことの確認に、わざと外のホストを入れる。
func release(tag string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"browser_download_url":"https://evil.example/x"}],"body":"SECRET-NOTES"}`, tag)
	}
}

func status(code int, hdr ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i+1 < len(hdr); i += 2 {
			w.Header().Set(hdr[i], hdr[i+1])
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"message":"SECRET-BODY"}`))
	}
}

func newChecker(t *testing.T, current string, h http.HandlerFunc) (*updatecheck.Checker, *updatechecktest.Origin) {
	t.Helper()
	o := updatechecktest.New(t)
	o.HandleFunc("api.github.com", h)
	return updatecheck.New(updatecheck.Options{Current: current, Transport: o.Transport(), Timeout: 5 * time.Second}), o
}

// 版の比較と、最新側が vX.Y.Z でないときの release_invalid（FR-UPD-3・4）。
func TestCheckVersions(t *testing.T) {
	tests := []struct {
		name, current, latest string
		want                  updatecheck.Status
		wantReason            updatecheck.Reason
	}{
		{"新しい版がある", "v0.1.4", "v0.1.5", updatecheck.UpdateAvailable, ""},
		{"同じ版", "v0.1.5", "v0.1.5", updatecheck.UpToDate, ""},
		{"手元のほうが新しい", "v0.2.0", "v0.1.5", updatecheck.UpToDate, ""},
		{"桁が増えても数として比べる", "v0.1.9", "v0.1.10", updatecheck.UpdateAvailable, ""},
		{"現在がプレリリース形でも正式版のほうが新しい", "v1.2.0-rc.1", "v1.2.0", updatecheck.UpdateAvailable, ""},
		{"現在がプレリリース形で最新が古い", "v1.2.0-rc.1", "v1.1.9", updatecheck.UpToDate, ""},
		{"最新が v なし", "v0.1.4", "0.1.5", updatecheck.Unknown, updatecheck.ReasonReleaseInvalid},
		{"最新が vX.Y", "v0.1.4", "v0.2", updatecheck.Unknown, updatecheck.ReasonReleaseInvalid},
		{"最新がプレリリース形", "v0.1.4", "v0.2.0-rc.1", updatecheck.Unknown, updatecheck.ReasonReleaseInvalid},
		{"最新が先頭ゼロ", "v0.1.4", "v01.2.3", updatecheck.Unknown, updatecheck.ReasonReleaseInvalid},
		{"最新がビルドメタデータ付き", "v0.1.4", "v0.2.0+b1", updatecheck.Unknown, updatecheck.ReasonReleaseInvalid},
		{"最新が前後に空白", "v0.1.4", "v0.2.0\n", updatecheck.Unknown, updatecheck.ReasonReleaseInvalid},
		{"最新が空", "v0.1.4", "", updatecheck.Unknown, updatecheck.ReasonReleaseInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newChecker(t, tt.current, release(tt.latest))
			got := c.Check(context.Background())
			if got.Status != tt.want || got.Reason != tt.wantReason {
				t.Fatalf("Check = %+v, want %s/%s", got, tt.want, tt.wantReason)
			}
			if got.Status == updatecheck.Unknown && got.Latest != "" {
				t.Errorf("不正な版が Latest に残っています: %q", got.Latest)
			}
			if got.Status != updatecheck.Unknown && got.Latest != tt.latest {
				t.Errorf("Latest = %q, want %q", got.Latest, tt.latest)
			}
		})
	}
}

// dev ビルドと vX.Y.Z でない現在の版は、通信せず dev_build（FR-UPD-3）。
func TestCheckDevBuildDoesNotConnect(t *testing.T) {
	for _, cur := range []string{"dev", "", "v1.2", "v1.2.3+meta", "1.2.3", "v1.2.3-"} {
		t.Run(cur, func(t *testing.T) {
			c, o := newChecker(t, cur, release("v9.9.9"))
			got := c.Check(context.Background())
			if got.Status != updatecheck.Unknown || got.Reason != updatecheck.ReasonDevBuild {
				t.Fatalf("Check = %+v", got)
			}
			if n := len(o.Requests()); n != 0 {
				t.Errorf("dev ビルドが通信しました（%d 件）", n)
			}
		})
	}
}

// HTTP の状態と reason の対応（FR-UPD-4）。
func TestCheckClassifiesFailures(t *testing.T) {
	tests := []struct {
		name string
		h    http.HandlerFunc
		want updatecheck.Reason
	}{
		{"404 はリリースなし", status(404), updatecheck.ReasonNoRelease},
		{"429 は上限", status(429), updatecheck.ReasonRateLimit},
		{"403 で残り 0 は上限", status(403, "X-RateLimit-Remaining", "0"), updatecheck.ReasonRateLimit},
		{"403 で Retry-After は上限", status(403, "Retry-After", "60"), updatecheck.ReasonRateLimit},
		{"403 で残りが 0 でなければ拒否", status(403, "X-RateLimit-Remaining", "12"), updatecheck.ReasonAccess},
		{"403 だけは拒否", status(403), updatecheck.ReasonAccess},
		{"401 は拒否", status(401), updatecheck.ReasonAccess},
		{"500 は通信", status(500), updatecheck.ReasonNetwork},
		{"503 は通信", status(503), updatecheck.ReasonNetwork},
		{"不正な JSON", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{not json")) }, updatecheck.ReasonReleaseInvalid},
		{"JSON だが tag_name なし", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"name":"x"}`)) }, updatecheck.ReasonReleaseInvalid},
		{"本文が 1 MiB を超える", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"tag_name":"v9.9.9","pad":"` + strings.Repeat("a", 1<<20) + `"}`))
		}, updatecheck.ReasonReleaseInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newChecker(t, "v0.1.4", tt.h)
			got := c.Check(context.Background())
			if got.Status != updatecheck.Unknown || got.Reason != tt.want || got.Latest != "" {
				t.Fatalf("Check = %+v, want unknown/%s", got, tt.want)
			}
		})
	}
}

func TestCheckConnectionFailure(t *testing.T) {
	c := updatecheck.New(updatecheck.Options{Current: "v0.1.4", Transport: updatechecktest.RoundTripFunc(
		func(*http.Request) (*http.Response, error) { return nil, errors.New("dial tcp: SECRET-ADDR refused") })})
	got := c.Check(context.Background())
	if got.Status != updatecheck.Unknown || got.Reason != updatecheck.ReasonNetwork {
		t.Fatalf("Check = %+v", got)
	}
}

// 応答が返らなければ Timeout で諦めて network。
func TestCheckTimeout(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	o := updatechecktest.New(t)
	o.HandleFunc("api.github.com", func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	c := updatecheck.New(updatecheck.Options{Current: "v0.1.4", Transport: o.Transport(), Timeout: 50 * time.Millisecond})
	got := c.Check(context.Background())
	if got.Status != updatecheck.Unknown || got.Reason != updatecheck.ReasonNetwork {
		t.Fatalf("Check = %+v", got)
	}
}

// 要求の形（FR-UPD-1）: GET・https・api.github.com・固定パス・ヘッダー。トークンは送らない。
func TestCheckRequestShape(t *testing.T) {
	t.Setenv("SWARM_CONSOLE_TOKEN", "TOKEN-SHOULD-NOT-LEAK")
	t.Setenv("GITHUB_TOKEN", "GH-SHOULD-NOT-LEAK")
	var gotMethod string
	c, o := newChecker(t, "v0.1.4", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		release("v0.1.4")(w, r)
	})
	c.Check(context.Background())
	reqs := o.Requests()
	if len(reqs) != 1 {
		t.Fatalf("要求 %d 件, want 1", len(reqs))
	}
	r := reqs[0]
	if gotMethod != http.MethodGet || r.Scheme != "https" || r.Host != "api.github.com" || r.Path != apiPath {
		t.Errorf("要求 = %s %s://%s%s", gotMethod, r.Scheme, r.Host, r.Path)
	}
	if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("User-Agent") != "swarm-console/v0.1.4" {
		t.Errorf("ヘッダー = %v", r.Header)
	}
	for k, v := range r.Header {
		if strings.Contains(strings.ToLower(k), "auth") || strings.Contains(strings.Join(v, ""), "SHOULD-NOT-LEAK") {
			t.Errorf("認証情報になりうるヘッダー: %s: %v", k, v)
		}
	}
}

// リダイレクトは api.github.com の https の中だけ追う。外へは追わず、外のホストへ要求も出さない（FR-UPD-1）。
func TestCheckRedirects(t *testing.T) {
	t.Run("api.github.com の中は追う", func(t *testing.T) {
		o := updatechecktest.New(t)
		o.HandleFunc("api.github.com", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == apiPath {
				http.Redirect(w, r, "https://api.github.com/repositories/1/releases/latest", http.StatusMovedPermanently)
				return
			}
			release("v0.1.5")(w, r)
		})
		c := updatecheck.New(updatecheck.Options{Current: "v0.1.4", Transport: o.Transport()})
		if got := c.Check(context.Background()); got.Status != updatecheck.UpdateAvailable {
			t.Fatalf("Check = %+v", got)
		}
	})
	for _, target := range []string{"https://evil.example/latest", "http://api.github.com/latest", "https://github.com/latest"} {
		t.Run("外は追わない "+target, func(t *testing.T) {
			o := updatechecktest.New(t)
			o.HandleFunc("api.github.com", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target, http.StatusFound) })
			o.HandleFunc("evil.example", release("v9.9.9"))
			o.HandleFunc("github.com", release("v9.9.9"))
			c := updatecheck.New(updatecheck.Options{Current: "v0.1.4", Transport: o.Transport()})
			got := c.Check(context.Background())
			if got.Status != updatecheck.Unknown || got.Reason != updatecheck.ReasonNetwork {
				t.Fatalf("Check = %+v", got)
			}
			for _, r := range o.Requests() {
				if r.Host != "api.github.com" || r.Scheme != "https" {
					t.Errorf("外へ要求が出ました: %s://%s", r.Scheme, r.Host)
				}
			}
		})
	}
	t.Run("追いすぎない", func(t *testing.T) {
		o := updatechecktest.New(t)
		o.HandleFunc("api.github.com", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://api.github.com/loop", http.StatusFound)
		})
		c := updatecheck.New(updatecheck.Options{Current: "v0.1.4", Transport: o.Transport()})
		if got := c.Check(context.Background()); got.Reason != updatecheck.ReasonNetwork {
			t.Fatalf("Check = %+v", got)
		}
		if n := len(o.Requests()); n > 7 {
			t.Errorf("リダイレクトを %d 回追いました", n)
		}
	})
}

// ctx が終わると、止まっている確認も中断する。
func TestCheckCancel(t *testing.T) {
	started := make(chan struct{})
	o := updatechecktest.New(t)
	o.HandleFunc("api.github.com", func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	c := updatecheck.New(updatecheck.Options{Current: "v0.1.4", Transport: o.Transport(), Timeout: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan updatecheck.Result, 1)
	go func() { done <- c.Check(ctx) }()
	<-started
	cancel()
	select {
	case got := <-done:
		if got.Status != updatecheck.Unknown {
			t.Errorf("Check = %+v", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancel で確認が戻りません")
	}
}

func TestVersionHelpers(t *testing.T) {
	for s, want := range map[string]bool{"v0.1.5": true, "v10.20.30": true, "v0.0.0": true, "v1.2": false, "1.2.3": false, "v1.2.3-rc.1": false, "v01.2.3": false, "v1.2.3\n": false} {
		if got := updatecheck.ValidTag(s); got != want {
			t.Errorf("ValidTag(%q) = %v", s, got)
		}
	}
	for s, want := range map[string]bool{"v1.2.3": true, "v1.2.0-rc.1": true, "dev": false, "v1.2": false, "v1.2.3+x": false, "": false} {
		if got := updatecheck.ValidCurrent(s); got != want {
			t.Errorf("ValidCurrent(%q) = %v", s, got)
		}
	}
	if !updatecheck.Newer("v0.1.10", "v0.1.9") || updatecheck.Newer("v0.1.9", "v0.1.9") || updatecheck.Newer("bad", "v0.1.9") {
		t.Error("Newer が semver どおりでない")
	}
	if got := updatecheck.ReleaseURL("v0.1.5"); got != "https://github.com/ousiassllc/swarm-console/releases/tag/v0.1.5" {
		t.Errorf("ReleaseURL = %q", got)
	}
}
