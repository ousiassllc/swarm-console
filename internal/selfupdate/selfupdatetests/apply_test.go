package selfupdatetests

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/selfupdate"
)

// 成功：入れ替え・.old（旧バイナリのハードリンク）・マーカー・終了の要求（FR-UPD-15・16）。
func TestApplySuccess(t *testing.T) {
	g := newRig(t)
	g.serve(okRelease(newVersion, newContent))
	before, err := os.Stat(g.exe)
	if err != nil {
		t.Fatal(err)
	}
	// 前回の残り（.new・.old）は消してから作り直される。
	_ = os.WriteFile(filepath.Join(g.dir, newName), []byte("stale"), 0o600)
	_ = os.WriteFile(filepath.Join(g.dir, oldName), []byte("older"), 0o600)

	if err := g.apply(newVersion); err != nil {
		t.Fatal(err)
	}
	if got := g.read(binName); got != newContent {
		t.Errorf("本体 = %q", got)
	}
	if fi, _ := os.Stat(g.exe); fi.Mode().Perm() != 0o755 {
		t.Errorf("本体の権限 = %v", fi.Mode().Perm())
	}
	oldInfo, err := os.Stat(filepath.Join(g.dir, oldName))
	if err != nil || !os.SameFile(before, oldInfo) || g.read(oldName) != oldContent {
		t.Errorf(".old が旧バイナリ（同じ inode）でない: %v", err)
	}
	var p pending
	if err := json.Unmarshal([]byte(g.read(pendingName)), &p); err != nil || p != (pending{From: curVersion, To: newVersion, Boots: 0}) {
		t.Errorf("マーカー = %+v (%v)", p, err)
	}
	if fi, _ := os.Stat(filepath.Join(g.dir, pendingName)); fi.Mode().Perm() != 0o600 {
		t.Errorf("マーカーの権限 = %v", fi.Mode().Perm())
	}
	if got := g.files(); !slices.Equal(got, []string{binName, oldName, pendingName}) {
		t.Errorf("置き場のファイル = %v（.new・一時ファイルが残っています）", got)
	}
	if n := g.exits.Load(); n != 1 {
		t.Errorf("終了の要求 = %d 回, want 1", n)
	}
	g.wantApply(selfupdate.StateIdle, "")
}

// 要求は SHA256SUMS → バイナリの 2 本だけ。検証済みの tag から組み立てた github.com の https で、認証は付けない（FR-UPD-13）。
func TestApplyRequestsOnlyDerivedURLs(t *testing.T) {
	t.Setenv("SWARM_CONSOLE_TOKEN", "TOKEN-SHOULD-NOT-LEAK")
	t.Setenv("GITHUB_TOKEN", "GH-SHOULD-NOT-LEAK")
	g := newRig(t)
	g.serve(okRelease(newVersion, newContent))
	if err := g.apply(newVersion); err != nil {
		t.Fatal(err)
	}
	reqs := g.origin.Requests()
	prefix := "/ousiassllc/swarm-console/releases/download/v0.1.5/"
	if len(reqs) != 2 || reqs[0].Path != prefix+"SHA256SUMS" || reqs[1].Path != prefix+"swarm-console_v0.1.5_linux_amd64" {
		t.Fatalf("要求 = %+v", reqs)
	}
	for _, r := range reqs {
		if r.Scheme != "https" || r.Host != "github.com" {
			t.Errorf("宛先 = %s://%s", r.Scheme, r.Host)
		}
		for k, v := range r.Header {
			if strings.Contains(strings.ToLower(k), "auth") || strings.Contains(strings.Join(v, ""), "SHOULD-NOT-LEAK") {
				t.Errorf("認証情報になりうるヘッダー %s: %v", k, v)
			}
		}
	}
}

// 実体は許可ホスト（objects.githubusercontent.com・release-assets.githubusercontent.com）へのリダイレクトで取れる。
func TestApplyFollowsAllowedRedirects(t *testing.T) {
	for _, host := range []string{"objects.githubusercontent.com", "release-assets.githubusercontent.com"} {
		t.Run(host, func(t *testing.T) {
			g := newRig(t)
			r := okRelease(newVersion, newContent)
			redirect := func(path string) func(http.ResponseWriter, *http.Request) {
				return func(w http.ResponseWriter, req *http.Request) {
					http.Redirect(w, req, "https://"+host+path, http.StatusFound)
				}
			}
			r.sums, r.binary = redirect("/s"), redirect("/b")
			g.serve(r)
			g.origin.HandleFunc(host, func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/s" {
					_, _ = w.Write([]byte(sumsLine(newVersion, newContent)))
					return
				}
				_, _ = w.Write([]byte(newContent))
			})
			if err := g.apply(newVersion); err != nil {
				t.Fatal(err)
			}
			if g.read(binName) != newContent {
				t.Error("入れ替わっていません")
			}
		})
	}
}

// 警告は reason が前回と変わったときだけ（FR-UPD-18）。URL・応答は出さない。
func TestApplyWarnsOnlyWhenReasonChanges(t *testing.T) {
	g := newRig(t)
	g.serve(release{tag: newVersion, sums: code(500), binary: body(newContent)})
	for range 3 {
		wantFailure(t, g.apply(newVersion), selfupdate.ReasonDownload)
	}
	g.serve(release{tag: newVersion, sums: code(404), binary: body(newContent)})
	wantFailure(t, g.apply(newVersion), selfupdate.ReasonAssetMissing)
	logs := g.logs.String()
	if n := strings.Count(logs, "自己更新できませんでした"); n != 2 {
		t.Errorf("警告 %d 回, want 2（download → asset_missing）\n%s", n, logs)
	}
	if n := strings.Count(logs, "自己更新を始めます"); n != 4 {
		t.Errorf("開始のログ %d 回, want 4", n)
	}
	if strings.Contains(logs, "SECRET") || strings.Contains(logs, "http") {
		t.Errorf("ログに URL・応答が出ています:\n%s", logs)
	}
}

func TestReset(t *testing.T) {
	g := newRig(t)
	g.serve(release{tag: newVersion, sums: code(500), binary: body(newContent)})
	wantFailure(t, g.apply(newVersion), selfupdate.ReasonDownload)
	g.u.Reset()
	g.wantApply(selfupdate.StateIdle, "")
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
