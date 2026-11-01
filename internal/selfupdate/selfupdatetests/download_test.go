package selfupdatetests

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/selfupdate"
)

// 404 は asset_missing、そのほかの HTTP の失敗は download。
func TestApplyHTTPFailures(t *testing.T) {
	tests := []struct {
		name string
		r    release
		want Reason
	}{
		{"SHA256SUMS が 404", release{sums: code(404), binary: body(newContent)}, selfupdate.ReasonAssetMissing},
		{"バイナリが 404", release{sums: body(sumsLine(newVersion, newContent)), binary: code(404)}, selfupdate.ReasonAssetMissing},
		{"SHA256SUMS が 500", release{sums: code(500), binary: body(newContent)}, selfupdate.ReasonDownload},
		{"バイナリが 503", release{sums: body(sumsLine(newVersion, newContent)), binary: code(503)}, selfupdate.ReasonDownload},
		{"バイナリが 403", release{sums: body(sumsLine(newVersion, newContent)), binary: code(403)}, selfupdate.ReasonDownload},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newRig(t)
			tt.r.tag = newVersion
			g.serve(tt.r)
			wantFailure(t, g.apply(newVersion), tt.want)
			g.assertUntouched()
			g.wantApply(selfupdate.StateFailed, tt.want)
			if logs := g.logs.String(); strings.Contains(logs, "SECRET") || strings.Contains(logs, "github.com") {
				t.Errorf("ログに応答・URL が出ています:\n%s", logs)
			}
		})
	}
}

// 本文が途中で切れた（Content-Length より短い）ときは download。
func TestApplyTruncatedBody(t *testing.T) {
	g := newRig(t)
	g.serve(release{tag: newVersion, sums: body(sumsLine(newVersion, newContent)), binary: func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("short"))
	}})
	wantFailure(t, g.apply(newVersion), selfupdate.ReasonDownload)
	g.assertUntouched()
}

// 応答が止まったままなら、全体のタイムアウトで download にして中断し、一時ファイルを消す。
func TestApplyTimeout(t *testing.T) {
	g := newRig(t, func(o *Options) { o.Timeout = 100 * time.Millisecond })
	g.serve(release{tag: newVersion, sums: body(sumsLine(newVersion, newContent)), binary: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}})
	wantFailure(t, g.apply(newVersion), selfupdate.ReasonDownload)
	g.assertUntouched()
}

// サイズ上限：SHA256SUMS は 64 KiB、バイナリは 64 MiB（超えたら中断して download）。
func TestApplySizeCaps(t *testing.T) {
	zeros := func(n int64, declare bool) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) {
			if declare {
				w.Header().Set("Content-Length", itoa(n))
			}
			buf := make([]byte, 32<<10)
			for left := n; left > 0; {
				m := min(int64(len(buf)), left)
				if _, err := w.Write(buf[:m]); err != nil {
					return
				}
				left -= m
			}
		}
	}
	tests := []struct {
		name string
		r    release
	}{
		{"SHA256SUMS が 64 KiB + 1（宣言あり）", release{sums: zeros(maxSumsBytes+1, true), binary: body(newContent)}},
		{"SHA256SUMS が 64 KiB + 1（宣言なし）", release{sums: zeros(maxSumsBytes+1, false), binary: body(newContent)}},
		{"バイナリが 64 MiB + 1（宣言あり）", release{sums: body(sumsLine(newVersion, newContent)), binary: zeros(maxBinaryBytes+1, true)}},
		{"バイナリが 64 MiB + 1（宣言なし）", release{sums: body(sumsLine(newVersion, newContent)), binary: zeros(maxBinaryBytes+1, false)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newRig(t)
			tt.r.tag = newVersion
			g.serve(tt.r)
			wantFailure(t, g.apply(newVersion), selfupdate.ReasonDownload)
			g.assertUntouched()
		})
	}
}

// ちょうど上限のバイナリは通る（上限は超えたときだけ）。
func TestApplyAcceptsBinaryAtCap(t *testing.T) {
	content := strings.Repeat("x", maxBinaryBytes)
	g := newRig(t)
	g.serve(okRelease(newVersion, content))
	if err := g.apply(newVersion); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(g.exe); err != nil || fi.Size() != maxBinaryBytes {
		t.Errorf("本体のサイズ = %v (%v)", fi, err)
	}
}

// 許可されないリダイレクト先（別ホスト・http・別ポート・多すぎる回数）へは追わず、要求も出さない（NFR-SEC-15）。
func TestApplyRefusesBadRedirects(t *testing.T) {
	targets := []string{
		"https://evil.example/swarm-console",
		"http://objects.githubusercontent.com/x",
		"https://objects.githubusercontent.com:8443/x",
		"https://api.github.com/x",
		"https://user:pw@github.com/x",
	}
	for _, target := range targets {
		for _, which := range []string{"sums", "binary"} {
			t.Run(which+" "+target, func(t *testing.T) {
				g := newRig(t)
				redirect := func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target, http.StatusFound) }
				r := okRelease(newVersion, newContent)
				if which == "sums" {
					r.sums = redirect
				} else {
					r.binary = redirect
				}
				g.serve(r)
				g.origin.HandleFunc("evil.example", body(newContent))
				wantFailure(t, g.apply(newVersion), selfupdate.ReasonDownload)
				g.assertUntouched()
				for _, req := range g.origin.Requests() {
					if req.Host != "github.com" {
						t.Errorf("許可外の宛先へ要求が出ました: %s://%s%s", req.Scheme, req.Host, req.Path)
					}
				}
			})
		}
	}
	t.Run("6 回を超えるリダイレクト", func(t *testing.T) {
		g := newRig(t)
		hops := 0
		r := okRelease(newVersion, newContent)
		r.sums = func(w http.ResponseWriter, req *http.Request) {
			hops++
			http.Redirect(w, req, "https://github.com/hop", http.StatusFound)
		}
		g.serve(r)
		g.origin.HandleFunc("github.com", func(w http.ResponseWriter, req *http.Request) {
			hops++
			http.Redirect(w, req, "https://github.com/hop", http.StatusFound)
		})
		wantFailure(t, g.apply(newVersion), selfupdate.ReasonDownload)
		g.assertUntouched()
		if hops > 6 {
			t.Errorf("リダイレクトを %d 回追いました（初回 + 5 回まで）", hops)
		}
	})
}

// 5 回のリダイレクトまでは追える。
func TestApplyFollowsFiveRedirects(t *testing.T) {
	g := newRig(t)
	r := okRelease(newVersion, newContent)
	g.serve(r)
	prefix := "/ousiassllc/swarm-console/releases/download/v0.1.5/"
	g.origin.HandleFunc("github.com", func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == prefix+"SHA256SUMS":
			http.Redirect(w, req, "https://github.com/hop/1", http.StatusFound)
		case strings.HasPrefix(req.URL.Path, "/hop/"):
			n := int(req.URL.Path[len("/hop/")] - '0')
			if n == 5 {
				_, _ = w.Write([]byte(sumsLine(newVersion, newContent)))
				return
			}
			http.Redirect(w, req, "https://github.com/hop/"+itoa(int64(n+1)), http.StatusFound)
		default:
			_, _ = w.Write([]byte(newContent))
		}
	})
	if err := g.apply(newVersion); err != nil {
		t.Fatal(err)
	}
}
