package render

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/templates"
)

// Templates は読み込み済みの report テンプレート（言語ごと）。作ったあと変更しない。
type Templates struct {
	byLang map[string]*template.Template
}

// LoadTemplates は組み込みの既定を読み、dir に report.<lang>.md があればそちらで置き換える。
// 構文エラー・実行時に壊れるテンプレート（存在しないフィールドの参照など）は、見本の入力で
// 一度描画して検出し、エラーにする（NFR-OPS-4。起動時に呼ぶ）。dir が空なら既定だけ。
func LoadTemplates(dir string) (*Templates, error) {
	t := &Templates{byLang: map[string]*template.Template{}}
	for _, lang := range []string{LangJA, LangEN} {
		name := "report." + lang + ".md"
		src, err := templates.FS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("render: 組み込みテンプレート %s: %w", name, err)
		}
		if dir != "" {
			custom, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // 設定フォルダ内の決まった名前のファイル
			switch {
			case err == nil:
				src = custom
			case !errors.Is(err, fs.ErrNotExist):
				return nil, fmt.Errorf("render: テンプレート %s を読めません: %w", name, err)
			}
		}
		tpl, err := template.New(name).Option("missingkey=error").Parse(string(src))
		if err != nil {
			return nil, fmt.Errorf("render: テンプレート %s の構文エラー: %w", name, err)
		}
		t.byLang[lang] = tpl
	}
	if err := t.selfCheck(); err != nil {
		return nil, err
	}
	return t, nil
}

// execute は lang のテンプレートを v で描画する。
func (t *Templates) execute(lang string, v *view) (string, error) {
	var buf bytes.Buffer
	if err := t.byLang[lang].Execute(&buf, v); err != nil {
		return "", fmt.Errorf("render: テンプレート %s の実行に失敗: %w", lang, err)
	}
	return buf.String(), nil
}

// selfCheck は見本のスナップショットを全言語で描画し、実行時エラーを起動時に出す。
func (t *Templates) selfCheck() error {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	snap := &pane.Snapshot{At: at, Panes: []pane.PaneState{{
		ID: "%1", Pos: "T1", Agent: "omp", State: agent.StateAsking, ReportExists: true,
		Note: &notes.Summary{From: "x", Repo: "o/r", Items: []string{"#1"}, Next: "n"},
	}}, Warnings: []pane.Warning{{Code: "layout_changed", Message: "m"}}}
	in := &ReportInput{
		Snapshot: snap, PaneID: "T1", Templates: t,
		Detail: &PaneDetail{
			Note:   &notes.Note{From: "x", Log: []notes.LogEntry{{At: "a", Text: "t"}}},
			Card:   &agent.Card{Question: "q", Options: []agent.Option{{N: 1, Label: "l", Recommended: true}}},
			Tail:   []string{"a"},
			Report: ReportFile{Exists: true, UpdatedAt: at},
		},
	}
	for lang := range t.byLang {
		if _, err := renderMarkdown(in, lang, &snap.Panes[0]); err != nil {
			return err
		}
		if _, err := renderMarkdown(in, lang, nil); err != nil {
			return err
		}
	}
	return nil
}
