package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// expandPath は先頭の "~" を home に展開し、絶対パスであることを確かめて Clean する。
// シンボリックリンクの解決はしない（resolvePath）。
func expandPath(key, p, home string) (string, error) {
	switch {
	case p == "":
		return "", fmt.Errorf("%s に空のパスがあります", key)
	case strings.IndexFunc(p, unicode.IsControl) >= 0:
		return "", fmt.Errorf("%s に制御文字を含むパスがあります", key)
	case p == "~":
		p = home
	case strings.HasPrefix(p, "~/"):
		p = filepath.Join(home, p[2:])
	case strings.HasPrefix(p, "~"):
		return "", fmt.Errorf("%s の ~ は ~/ の形だけ使えます（他ユーザーのホームは不可）", key)
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%s は絶対パスで書いてください", key)
	}
	return filepath.Clean(p), nil
}

// resolvePath は Clean 済みのパスが存在するときだけシンボリックリンクを解決する。無ければそのまま。
func resolvePath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(r)
	}
	return p
}

func expandList(key string, in []string, home string, resolve bool) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, p := range in {
		e, err := expandPath(key, p, home)
		if err != nil {
			return nil, err
		}
		if resolve {
			e = resolvePath(e)
		}
		out = append(out, e)
	}
	return out, nil
}

// buildPaths は allowed_roots・denied_paths・file_roots・disk_mounts・report_dir を既定値つきで決める。
// denied_paths には、設定の一覧にかかわらず ~/dotfiles を必ず含める（NFR-SEC-8）。
func buildPaths(r *rawConfig, c *Config, home string) error {
	var err error
	allowed := r.AllowedRoots
	if allowed == nil {
		allowed = []string{"/mnt/nvme/", "~/projects/"}
	}
	if len(allowed) == 0 {
		return errors.New("allowed_roots は 1 つ以上必要です")
	}
	if c.AllowedRoots, err = expandList("allowed_roots", allowed, home, true); err != nil {
		return err
	}
	files := r.FileRoots
	if files == nil {
		files = append([]string{"/tmp/"}, c.AllowedRoots...)
	}
	if c.FileRoots, err = expandList("file_roots", files, home, true); err != nil {
		return err
	}
	denied := append([]string{"~/dotfiles"}, r.DeniedPaths...)
	if c.DeniedPaths, err = expandList("denied_paths", denied, home, true); err != nil {
		return err
	}
	c.DeniedPaths = dedupe(c.DeniedPaths)
	mounts := r.DiskMounts
	if mounts == nil {
		mounts = []string{"/", "/mnt/nvme"}
	}
	if c.DiskMounts, err = expandList("disk_mounts", mounts, home, false); err != nil {
		return err
	}
	dir := r.ReportDir
	if dir == "" {
		dir = defaultReportDir
	}
	c.ReportDir, err = expandPath("report_dir", dir, home)
	return err
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
