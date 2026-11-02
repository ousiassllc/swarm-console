package rendertest

import (
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/render"
)

func TestDispatchGolden(t *testing.T) {
	for _, profile := range []string{"sweep", "fix", "investigate"} {
		for _, withCmd := range []bool{true, false} {
			for _, withExtra := range []bool{true, false} {
				in := render.DispatchInput{
					Task: "Issue #982 を実装する。\n詳細は指示ファイルを見る。", Profile: profile,
					Profiles: testProfiles(), ReportDir: reportDir, Pos: "B4",
				}
				name := "dispatch." + profile
				if withCmd {
					in.Command = "/issue-sweep #982 --single-pr --base main"
					name += ".cmd"
				}
				if withExtra {
					in.ExtraRules = []string{"dependabot PR に触らない", "報告先は {report_path}"}
					name += ".extra"
				}
				t.Run(name, func(t *testing.T) {
					got, err := render.Dispatch(in)
					if err != nil {
						t.Fatal(err)
					}
					golden(t, name+".txt", got)
				})
			}
		}
	}
}

// セッション名に「/」を含む位置でも、指示文のパスとサーバーが探すパス（pane.ReportPath）が同じになる。
func TestDispatchReportPathMatchesPane(t *testing.T) {
	for _, tc := range []struct{ pos, want string }{
		{"a/b:T1", "/r/a_b_T1.md"},
		{"work:B2", "/r/work_B2.md"},
	} {
		got, err := render.Dispatch(render.DispatchInput{
			Task: "T", Profile: "fix", Profiles: testProfiles(), ReportDir: "/r", Pos: tc.pos,
		})
		if err != nil {
			t.Fatal(err)
		}
		if p := pane.ReportPath("/r", tc.pos); p != tc.want || !strings.Contains(got, "`"+p+"`") {
			t.Errorf("pos %q: pane.ReportPath=%q want %q, 指示文:\n%s", tc.pos, p, tc.want, got)
		}
	}
}

func TestDispatchShape(t *testing.T) {
	got, err := render.Dispatch(render.DispatchInput{
		Command: "/x", Task: "T", Profile: "fix", ExtraRules: []string{"E"},
		Profiles: testProfiles(), ReportDir: "/r", Pos: "work:B2",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "/x\n\n## やること\nT\n\n## 条件\n" +
		"- コミットは必ず `git commit -s`（cherry-pick も `-s`）、マージは `git merge --signoff`\n" +
		"- マージ・Issue の close・デプロイはしない\n" +
		"- 最終報告を `/r/work_B2.md` にも書く\n" +
		"- PR は 1 本まで。バグの再現確認と回帰テストを含める。PR を出して CI が緑になったら止まる\n" +
		"- E\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "{report_path}") || !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Fatal("置換漏れ・末尾改行")
	}
}

func TestDispatchUnknownProfile(t *testing.T) {
	for _, p := range []string{"nope", "", "SWEEP"} {
		_, err := render.Dispatch(render.DispatchInput{Task: "t", Profile: p, Profiles: testProfiles()})
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != apperr.InvalidArgs || ae.Details["field"] != "profile" {
			t.Errorf("%q: %v", p, err)
		}
	}
}

func TestDispatchDeterministic(t *testing.T) {
	in := render.DispatchInput{Task: "t", Profile: "sweep", Profiles: testProfiles(), ReportDir: reportDir, Pos: "T1"}
	first, _ := render.Dispatch(in)
	for i := 0; i < 100; i++ {
		if got, _ := render.Dispatch(in); got != first {
			t.Fatal("出力が変わった")
		}
	}
}
