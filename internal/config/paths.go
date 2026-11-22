package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// ExpandPath は先頭の "~" を home に展開し、絶対パスであることを確かめて Clean する。
// シンボリックリンクの解決はしない（resolvePath）。
func ExpandPath(key, p, home string) (string, error) {
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
		e, err := ExpandPath(key, p, home)
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

// buildPaths は allowed_roots・denied_paths・file_roots・file_get_roots・file_get_max_bytes・disk_mounts・report_dir を既定値つきで決める。
// denied_paths の既定は ["~/dotfiles"]。config.toml に書いたら既定と入れ替える（足さない。P-29）。
func buildPaths(r *rawConfig, c *Config, home string) error {
	var err error
	allowed := r.AllowedRoots
	if allowed == nil {
		allowed = DefaultAllowedRoots()
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
	if c.FileGetRoots, err = expandList("file_get_roots", r.FileGetRoots, home, true); err != nil {
		return err
	}
	c.FileGetRoots = dedupe(c.FileGetRoots)
	if c.FileGetMaxBytes, err = checkFileGetMaxBytes(r.FileGetMaxBytes); err != nil {
		return err
	}
	denied := r.DeniedPaths
	if denied == nil {
		denied = DefaultDeniedPaths()
	}
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
	if c.ReportDir, err = ExpandPath("report_dir", dir, home); err != nil {
		return err
	}
	return checkReportDir(c.ReportDir)
}

// ReportRoot は report_dir を置ける唯一の根（NFR-SEC-14 の書き込み先 /tmp/swarm-console）。
const ReportRoot = "/tmp/swarm-console"

// checkReportDir は Clean 済みの report_dir が ReportRoot の下（ReportRoot 自身は不可）にあることを確かめる。
// 文字列の上（.. は Clean 済み）と、シンボリックリンクを解決した実パスの上の両方で見る（リンクで外へ出させない）。
func checkReportDir(dir string) error {
	if !isUnder(dir, ReportRoot) || !isUnder(resolveExisting(dir), resolveExisting(ReportRoot)) {
		return fmt.Errorf("report_dir は %s の下のフォルダにしてください（NFR-SEC-14。指定: %s）", ReportRoot, dir)
	}
	return nil
}

// isUnder は Clean 済みの p が root の厳密な下（root 自身は含まない）にあるかを返す。
func isUnder(p, root string) bool {
	return strings.HasPrefix(p, root+string(filepath.Separator))
}

// resolveExisting は p の存在する最も深い祖先のシンボリックリンクを解決し、残りをそのまま足す。
// まだ無いフォルダ（起動時に作る report_dir）でも、途中のリンクで外へ出るパスを見抜くため。
func resolveExisting(p string) string {
	rest := ""
	for cur := p; ; {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
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

// checkFileGetMaxBytes は file_get_max_bytes（省略は既定の 20 MiB）を検査する。1 以上 MaxFileGetMaxBytes 以下。
func checkFileGetMaxBytes(p *int64) (int64, error) {
	if p == nil {
		return DefaultFileGetMaxBytes, nil
	}
	if *p < 1 || *p > MaxFileGetMaxBytes {
		return 0, fmt.Errorf("file_get_max_bytes は 1 以上 %d 以下のバイト数にしてください", MaxFileGetMaxBytes)
	}
	return *p, nil
}
