package selfupdatetests

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/selfupdate"
)

type probeRig struct {
	t    *testing.T
	dir  string
	clk  *clock.Fake
	logs *lockedBuf
}

func newProbe(t *testing.T) *probeRig {
	t.Helper()
	dir := selfupdate.Dir(t.TempDir())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := &probeRig{t: t, dir: dir, clk: clock.NewFake(t0), logs: &lockedBuf{}}
	p.write(binName, "NEW-BINARY")
	return p
}

func (p *probeRig) write(name, content string) {
	p.t.Helper()
	if err := os.WriteFile(filepath.Join(p.dir, name), []byte(content), 0o600); err != nil {
		p.t.Fatal(err)
	}
}

func (p *probeRig) read(name string) string {
	b, err := os.ReadFile(filepath.Join(p.dir, name)) //nolint:gosec // テストが作った一時ディレクトリ
	if err != nil {
		return ""
	}
	return string(b)
}

func (p *probeRig) exists(name string) bool {
	_, err := os.Lstat(filepath.Join(p.dir, name))
	return err == nil
}

func (p *probeRig) probation(version string) selfupdate.Decision {
	return selfupdate.Probation(p.dir, version, p.clk, slog.New(slog.NewJSONHandler(p.logs, nil)))
}

func (p *probeRig) boots() int {
	var m pending
	if err := json.Unmarshal([]byte(p.read(pendingName)), &m); err != nil {
		p.t.Fatalf("マーカーを読めません: %v", err)
	}
	return m.Boots
}

const marker0 = `{"from":"v0.1.4","to":"v0.1.5","boots":0}`

// マーカーが無ければ通常どおりで、何も作らない。
func TestProbationNoMarker(t *testing.T) {
	p := newProbe(t)
	if d := p.probation("v0.1.5"); d.Rollback || d.Trial != nil {
		t.Fatalf("selfupdate.Decision = %+v", d)
	}
	if p.exists(pendingName) || p.exists(failedName) {
		t.Error("ファイルが作られました")
	}
}

// 1・2 回目の起動は boots を数えて試運転、3 回目の起動で .old を戻して終了コード 1 の印（FR-UPD-17）。
func TestProbationBootsThenRollback(t *testing.T) {
	p := newProbe(t)
	p.write(pendingName, marker0)
	p.write(oldName, "OLD-BINARY")
	for want := 1; want <= 2; want++ {
		d := p.probation("v0.1.5")
		if d.Rollback || d.Trial == nil || d.Trial.From != "v0.1.4" || d.Trial.To != "v0.1.5" {
			t.Fatalf("%d 回目: selfupdate.Decision = %+v", want, d)
		}
		if got := p.boots(); got != want {
			t.Fatalf("%d 回目: boots = %d", want, got)
		}
		if p.read(binName) != "NEW-BINARY" {
			t.Fatalf("%d 回目: 戻すのが早すぎます", want)
		}
	}
	d := p.probation("v0.1.5")
	if !d.Rollback || d.Trial != nil {
		t.Fatalf("3 回目: selfupdate.Decision = %+v", d)
	}
	if p.read(binName) != "OLD-BINARY" || p.exists(oldName) || p.exists(pendingName) {
		t.Errorf(".old が戻っていない: bin=%q old=%v marker=%v", p.read(binName), p.exists(oldName), p.exists(pendingName))
	}
	var f failed
	if err := json.Unmarshal([]byte(p.read(failedName)), &f); err != nil || f.Version != "v0.1.5" || f.At != t0.Format(time.RFC3339) {
		t.Errorf(".update-failed = %q (%v)", p.read(failedName), err)
	}
	if fi, _ := os.Stat(filepath.Join(p.dir, failedName)); fi.Mode().Perm() != 0o600 {
		t.Errorf(".update-failed の権限 = %v", fi.Mode().Perm())
	}
	logs := p.logs.String()
	if !strings.Contains(logs, `"level":"ERROR"`) || !strings.Contains(logs, `"from":"v0.1.4"`) || !strings.Contains(logs, `"to":"v0.1.5"`) {
		t.Errorf("ERROR ログ（from・to）がありません:\n%s", logs)
	}
	// 戻ったあとの旧バイナリ（版 v0.1.4）は、マーカーが無いので通常どおり起動する。
	if d := p.probation("v0.1.4"); d.Rollback || d.Trial != nil {
		t.Errorf("戻したあとの起動 = %+v", d)
	}
}

// boots がすでに 2 のマーカーなら、次の起動（増やして 3）が戻し。
func TestProbationRollsBackAtThirdBootFromFile(t *testing.T) {
	p := newProbe(t)
	p.write(pendingName, `{"from":"v0.1.4","to":"v0.1.5","boots":2}`)
	p.write(oldName, "OLD-BINARY")
	if d := p.probation("v0.1.5"); !d.Rollback {
		t.Fatalf("selfupdate.Decision = %+v", d)
	}
}

// to が自分の版でない（入れ替えが起きなかった・戻したあと）マーカーは消して通常どおり。
func TestProbationMarkerForOtherVersionIsDeleted(t *testing.T) {
	p := newProbe(t)
	p.write(pendingName, `{"from":"v0.1.4","to":"v0.1.5","boots":2}`)
	p.write(oldName, "OLD-BINARY")
	if d := p.probation("v0.1.4"); d.Rollback || d.Trial != nil {
		t.Fatalf("selfupdate.Decision = %+v", d)
	}
	if p.exists(pendingName) || !p.exists(oldName) || p.read(binName) != "NEW-BINARY" || p.exists(failedName) {
		t.Error("マーカーだけが消えるはず")
	}
}

// 読めない・形が違うマーカーは消して通常どおり（ER §8）。
func TestProbationInvalidMarkerIsDeleted(t *testing.T) {
	for name, body := range map[string]string{
		"壊れた JSON": "{", "空": "", "版の形が違う": `{"from":"v0.1.4","to":"v0.1","boots":0}`, "boots が負": `{"from":"v0.1.4","to":"v0.1.5","boots":-1}`,
		"未知のキー": `{"from":"v0.1.4","to":"v0.1.5","boots":0,"x":1}`, "boots が文字列": `{"from":"v0.1.4","to":"v0.1.5","boots":"1"}`,
		"to がプレリリース形": `{"from":"v0.1.4","to":"v0.1.5-rc.1","boots":0}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := newProbe(t)
			p.write(pendingName, body)
			p.write(oldName, "OLD-BINARY")
			if d := p.probation("v0.1.5"); d.Rollback || d.Trial != nil {
				t.Fatalf("selfupdate.Decision = %+v", d)
			}
			if p.exists(pendingName) || p.read(binName) != "NEW-BINARY" {
				t.Error("マーカーが残る、または本体が変わりました")
			}
		})
	}
}

// .old が無い・戻せないとき、マーカーを消して通常どおり起動を続け（以後は数えない）、ERROR ログを出す。
func TestProbationRollbackFailureContinues(t *testing.T) {
	for name, setup := range map[string]func(*probeRig){
		".old が無い": func(*probeRig) {},
		"本体が空でないディレクトリで rename できない": func(p *probeRig) {
			_ = os.Remove(filepath.Join(p.dir, binName))
			_ = os.MkdirAll(filepath.Join(p.dir, binName, "x"), 0o700)
			p.write(oldName, "OLD-BINARY")
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProbe(t)
			setup(p)
			p.write(pendingName, `{"from":"v0.1.4","to":"v0.1.5","boots":2}`)
			d := p.probation("v0.1.5")
			if d.Rollback || d.Trial != nil {
				t.Fatalf("selfupdate.Decision = %+v", d)
			}
			if p.exists(pendingName) || p.exists(failedName) {
				t.Error("マーカーが残る、または .update-failed を書きました")
			}
			if !strings.Contains(p.logs.String(), `"level":"ERROR"`) {
				t.Errorf("ERROR ログがありません:\n%s", p.logs.String())
			}
			if d := p.probation("v0.1.5"); d.Trial != nil || d.Rollback {
				t.Errorf("以後は数えないはず: %+v", d)
			}
		})
	}
}

// 待ち受けを始めてから 60 秒（clock）生きたらマーカーを消す。それより前に終了すれば残る。
func TestTrialConfirmsAfter60Seconds(t *testing.T) {
	p := newProbe(t)
	p.write(pendingName, marker0)
	d := p.probation("v0.1.5")
	ready := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- d.Trial.Run(ctx, p.clk, ready) }()

	// 待ち受け前は時計が進んでも確定しない（待ちにも入らない）。
	p.clk.Advance(time.Hour)
	if p.clk.Waiters() != 0 || !p.exists(pendingName) {
		t.Fatal("待ち受け前に確定しようとしました")
	}
	close(ready)
	eventually(t, "60 秒の待ちに入る", func() bool { return p.clk.Waiters() == 1 })
	p.clk.Advance(59 * time.Second)
	if !p.exists(pendingName) {
		t.Fatal("59 秒で確定しました")
	}
	p.clk.Advance(time.Second)
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("確定しませんでした")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("確定しません")
	}
	if p.exists(pendingName) {
		t.Error("マーカーが残っています")
	}
	if !strings.Contains(p.logs.String(), `"level":"INFO"`) || !strings.Contains(p.logs.String(), "試運転が済みました") {
		t.Errorf("INFO ログがありません:\n%s", p.logs.String())
	}
}

// 60 秒より前の終了（SIGTERM を含む）ではマーカーが残り、次の起動で boots が増える。
func TestTrialKeepsMarkerWhenStoppedEarly(t *testing.T) {
	for _, stopAfterReady := range []bool{false, true} {
		p := newProbe(t)
		p.write(pendingName, marker0)
		d := p.probation("v0.1.5")
		ready := make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan bool, 1)
		go func() { done <- d.Trial.Run(ctx, p.clk, ready) }()
		if stopAfterReady {
			close(ready)
			eventually(t, "60 秒の待ちに入る", func() bool { return p.clk.Waiters() == 1 })
			p.clk.Advance(30 * time.Second)
		}
		cancel()
		if ok := <-done; ok {
			t.Fatal("確定してはいけません")
		}
		if got := p.boots(); got != 1 {
			t.Errorf("boots = %d", got)
		}
		if d2 := p.probation("v0.1.5"); d2.Trial == nil || p.boots() != 2 {
			t.Errorf("次の起動 = %+v boots=%d", d2, p.boots())
		}
	}
}
