package ops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

// dir_list の limit（API 仕様 endpoints-pane.md §5.2）。
const (
	dirListDefaultLimit = 200
	dirListMaxLimit     = 500
)

type dirListArgs struct {
	Path  *string `json:"path"`
	Limit *int    `json:"limit"`
}

type dirEntryJSON struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Git  bool   `json:"git"`
}

type dirListJSON struct {
	Path      string         `json:"path,omitempty"` // path を省略したときは出さない
	Entries   []dirEntryJSON `json:"entries"`
	Truncated bool           `json:"truncated"`
}

func (e *env) dirListTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "dir_list",
		Description: descDirList,
		Scope:       toolkit.ScopeOps,
		Schema: toolkit.Object(nil, toolkit.Props{
			"path":  toolkit.StrProp("一覧するディレクトリの絶対パス（allowed_roots の下だけ）。省略すると allowed_roots そのものを返す"),
			"limit": toolkit.IntProp("返す件数の上限（既定 200）", 1, dirListMaxLimit),
		}),
		Handler: e.dirList,
	}
}

func (e *env) dirList(ctx context.Context, _ auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a dirListArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	limit := dirListDefaultLimit
	if a.Limit != nil {
		if limit = *a.Limit; limit < 1 || limit > dirListMaxLimit {
			return toolkit.Result{}, toolkit.InvalidArg("limit")
		}
	}
	au := toolkit.AuditOf(ctx)
	out := dirListJSON{Entries: []dirEntryJSON{}}
	var candidates []string // 載せるか決める前の、絶対パスの候補（名前順）
	if a.Path == nil {
		candidates = e.rootCandidates()
	} else {
		if *a.Path == "" {
			return toolkit.Result{}, toolkit.InvalidArg("path")
		}
		au.SetPath(*a.Path)
		dir, err := e.Safety.CheckDirList(*a.Path)
		if err != nil {
			return toolkit.Result{}, apperr.As(err)
		}
		au.SetPath(dir)
		out.Path = dir
		if candidates, err = childCandidates(dir); err != nil {
			return toolkit.Result{}, err
		}
	}
	for _, p := range candidates {
		if !e.Safety.IsListableDir(p) {
			continue
		}
		if len(out.Entries) == limit {
			out.Truncated = true
			break
		}
		out.Entries = append(out.Entries, dirEntryJSON{Name: filepath.Base(p), Path: p, Git: hasGit(p)})
	}
	return toolkit.MarshalResult(out)
}

// rootCandidates は allowed_roots（設定で正規化済みの絶対パス）を名前順に並べて返す。
func (e *env) rootCandidates() []string {
	roots := append([]string(nil), e.Cfg.AllowedRoots...)
	sort.Slice(roots, func(i, j int) bool {
		if bi, bj := filepath.Base(roots[i]), filepath.Base(roots[j]); bi != bj {
			return bi < bj
		}
		return roots[i] < roots[j]
	})
	return roots
}

// childCandidates は dir の直下の、隠し（"." 始まり）でない子の絶対パスを名前順に返す。
// ファイルは除くが、シンボリックリンクは宛先がディレクトリかを調べるまで残す（判定は IsListableDir）。
func childCandidates(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir) // ファイル名の昇順
	if err != nil {
		if os.IsNotExist(err) {
			return nil, apperr.New(apperr.NotFound, "field", "path")
		}
		return nil, apperr.New(apperr.PathForbidden, "field", "path", "reason", "unreadable")
	}
	paths := make([]string, 0, len(ents))
	for _, ent := range ents {
		isDirOrLink := ent.IsDir() || ent.Type()&os.ModeSymlink != 0
		if isDirOrLink && !strings.HasPrefix(ent.Name(), ".") {
			paths = append(paths, filepath.Join(dir, ent.Name()))
		}
	}
	return paths, nil
}

// hasGit は dir の直下に .git（ディレクトリでもファイルでも。worktree は .git ファイル）があるか。
func hasGit(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}
