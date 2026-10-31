package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTpl(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTemplatesDefaultsAndEmptyDir(t *testing.T) {
	if _, err := LoadTemplates(""); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTemplates(t.TempDir()); err != nil {
		t.Fatalf("同名ファイルが無ければ既定: %v", err)
	}
}

func TestLoadTemplatesRejectsBroken(t *testing.T) {
	cases := map[string]string{
		"syntax":        "{{if .At}}",
		"unknown field": "{{.NoSuchField}}",
		"bad range":     "{{range .At}}x{{end}}",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeTpl(t, dir, "report.en.md", body)
			if _, err := LoadTemplates(dir); err == nil || !strings.Contains(err.Error(), "report.en.md") {
				t.Fatalf("起動時に検出されない: %v", err)
			}
		})
	}
}

func TestCustomTemplateOverridesPerLang(t *testing.T) {
	dir := t.TempDir()
	writeTpl(t, dir, "report.ja.md", "独自 {{.At}}\n\n\n")
	tpl, err := LoadTemplates(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := mustReport(t, ReportInput{Snapshot: calmSnapshot(), Lang: "ja", Templates: tpl})
	if got != "独自 2026-10-31 22:10:05 (+09:00)\n" {
		t.Fatalf("%q", got)
	}
	en := mustReport(t, ReportInput{Snapshot: calmSnapshot(), Lang: "en", Templates: tpl})
	if !strings.HasPrefix(en, "# swarm-console report") {
		t.Fatal("en は既定のまま")
	}
	viaDir := mustReport(t, ReportInput{Snapshot: calmSnapshot(), Lang: "ja", TemplatesDir: dir})
	if viaDir != got {
		t.Fatal("TemplatesDir 経由でも同じ")
	}
}

func TestMemoFreeTextIsNotTemplateExpanded(t *testing.T) {
	snap := fixedSnapshot()
	snap.Panes[3].Note.Next = "{{.At}} {{ template }}"
	out := mustReport(t, ReportInput{Snapshot: snap})
	if !strings.Contains(out, "{{.At}} {{ template }}") {
		t.Fatal("メモの中身は書かれたまま出す")
	}
}
