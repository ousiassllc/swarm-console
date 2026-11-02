package integration

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/app"
	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// 起動拒否: 危ない・壊れた設定は app.New が起動前にエラーで拒否する（待ち受けも tmux へのアクセスもしない）。
func TestStartupRefusals(t *testing.T) {
	tests := []struct {
		name  string
		opts  harness.Options
		setup func(h *harness.H)
		want  string // エラーに含まれる語（原因の箇所）
	}{
		{"listen_addr 0.0.0.0", harness.Options{Config: map[string]string{"listen_addr": `"0.0.0.0:48120"`}}, nil, "listen_addr"},
		{"listen_addr [::]", harness.Options{Config: map[string]string{"listen_addr": `"[::]:48120"`}}, nil, "listen_addr"},
		{"listen_addr 未指定", harness.Options{Config: map[string]string{"listen_addr": ""}}, nil, "listen_addr"},
		{"listen_addr ホスト名", harness.Options{Config: map[string]string{"listen_addr": `"localhost:48120"`}}, nil, "listen_addr"},
		{"listen_addr ポートなし", harness.Options{Config: map[string]string{"listen_addr": `"127.0.0.1"`}}, nil, "listen_addr"},
		{"listen_addr tailnet 外の IP", harness.Options{Config: map[string]string{"listen_addr": `"192.168.1.10:48120"`}}, nil, "listen_addr"},
		{"listen_addr tailnet の IP で 48120 以外のポート", harness.Options{Config: map[string]string{"listen_addr": `"100.64.0.1:8080"`}}, nil, "listen_addr"},
		{"未知のキー", harness.Options{Config: map[string]string{"listen_adr": `"127.0.0.1:1"`}}, nil, "listen_adr"},
		{"期間が不正", harness.Options{Config: map[string]string{"poll_interval": `"はやい"`}}, nil, "poll_interval"},
		{"tokens.toml が 600 でない", harness.Options{}, func(h *harness.H) {
			//nolint:gosec // 緩い権限の tokens.toml を起動拒否させる検証
			if err := os.Chmod(h.TokensPath(), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "tokens.toml"},
		{"tokens.toml が壊れている", harness.Options{}, func(h *harness.H) { h.WriteFile(h.TokensPath(), "[[token]\nname = ") }, "tokens.toml"},
		{"テンプレートの構文エラー", harness.Options{}, func(h *harness.H) {
			h.WriteFile(filepath.Join(h.Dir, "templates", "report.ja.md"), "# {{ .Title \n")
		}, "report.ja.md"},
		{"テンプレートが存在しないフィールドを参照", harness.Options{}, func(h *harness.H) {
			h.WriteFile(filepath.Join(h.Dir, "templates", "report.en.md"), "# {{ .NoSuchField }}\n")
		}, "report.en.md"},
		{"不正な正規表現の [agents.omp]", harness.Options{Agents: map[string]map[string]string{"omp": {"working_re": `"("`}}}, nil, "working_re"},
		{"不正な process_match", harness.Options{Agents: map[string]map[string]string{"claude": {"process_match": `["["]`}}}, nil, "process_match"},
		{"profiles.toml に sweep がない", harness.Options{Profiles: "[common]\nrules = [\"a\"]\n"}, nil, "profile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.NoTmuxSession = true
			h := harness.Prepare(t, tt.opts)
			if tt.setup != nil {
				tt.setup(h)
			}
			a, err := app.New(h.Config, "test", slog.New(slog.DiscardHandler))
			if err == nil {
				t.Fatal("起動が拒否されない")
			}
			if a != nil {
				t.Errorf("エラーなのに App が返った")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("エラーに %q がありません: %v", tt.want, err)
			}
			for name := range map[string]bool{harness.OpsA: true, harness.ReportA: true} {
				if strings.Contains(err.Error(), h.Token(name)) {
					t.Errorf("エラーにトークンの平文が出ている")
				}
			}
		})
	}
}

// 待ち受けに失敗（ポートが使用中）したら Run はエラーで戻る（systemd の Restart=always に任せる）。
func TestRunFailsWhenPortInUse(t *testing.T) {
	first := harness.Start(t, harness.Options{NoTmuxSession: true})
	h := harness.Prepare(t, harness.Options{NoTmuxSession: true, Config: map[string]string{"listen_addr": `"` + first.Addr + `"`}})
	a, err := app.New(h.Config, "test", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.Run(ctx); err == nil || !strings.Contains(err.Error(), first.Addr) {
		t.Errorf("Run = %v", err)
	}
}
