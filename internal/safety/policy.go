// Package safety はパス検証（cwd・file）・貼り付けテキストの検証・秘密文字列の検出を扱う（FR-SAFE・NFR-SEC）。
// どの検証も、失敗したときは何も送らない前提で *apperr.Error を返す。details に入力の全文は入れない。
package safety

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/config"
)

// Policy は設定から作る不変の安全ポリシー。複数の goroutine から同時に使える。
type Policy struct {
	allowedRoots []string
	deniedPaths  []string
	fileRoots    []string
	// secretPaths は file_get が名前によらず断る、swarm-console 自身の設定・データのファイル（tokens・profiles・notes）。
	secretPaths []string
}

// NewPolicy は cfg の allowed_roots・denied_paths・file_roots と、tokens などの場所を写して Policy を作る。
func NewPolicy(cfg *config.Config) *Policy {
	p := &Policy{
		allowedRoots: append([]string(nil), cfg.AllowedRoots...),
		deniedPaths:  append([]string(nil), cfg.DeniedPaths...),
		fileRoots:    append([]string(nil), cfg.FileRoots...),
	}
	for _, s := range []string{cfg.TokensPath, cfg.ProfilesPath, cfg.NotesPath} {
		if s != "" {
			p.secretPaths = append(p.secretPaths, s)
		}
	}
	return p
}

// DetectSecret は Policy 越しに使うための DetectSecret（パッケージ関数）への委譲。
func (p *Policy) DetectSecret(s string) bool { return DetectSecret(s) }

// CheckCWD は pane_start・dispatch の cwd を検証し、シンボリックリンク解決後の絶対パスを返す。
//   - 空・相対パス・制御文字・改行・"'" を含む → invalid_args（cwd はシェルに渡すので "'" も拒否する）
//   - 存在しない・ディレクトリでない・allowed_roots の外・denied_paths の一致または配下 → forbidden_path
func (p *Policy) CheckCWD(path string) (string, error) {
	if err := checkPathSyntax("cwd", path, true); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", forbidden("cwd", "not_found")
	}
	resolved = filepath.Clean(resolved)
	if !isDir(resolved) {
		return "", forbidden("cwd", "not_directory")
	}
	if underAny(resolved, p.deniedPaths) {
		return "", forbidden("cwd", "denied")
	}
	if !underAny(resolved, p.allowedRoots) {
		return "", forbidden("cwd", "outside_roots")
	}
	return resolved, nil
}

// CheckFile は send_text の file を検証し、シンボリックリンク解決後の絶対パスを返す。
//   - 空・相対パス・制御文字を含む → invalid_args
//   - file_roots の外、または denied_paths の一致・配下 → forbidden_path（存在しないファイルも、字面が根の外・拒否の下なら forbidden_path で存在を漏らさない）
//   - 根の下だが存在しない・一般ファイルでない → file_unreadable
func (p *Policy) CheckFile(path string) (string, error) {
	if err := checkPathSyntax("file", path, false); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if !underAny(filepath.Clean(path), p.fileRoots) {
			return "", forbidden("file", "outside_roots")
		}
		if underAny(filepath.Clean(path), p.deniedPaths) {
			return "", forbidden("file", "denied")
		}
		return "", apperr.New(apperr.FileUnreadable, "field", "file")
	}
	resolved = filepath.Clean(resolved)
	if !underAny(resolved, p.fileRoots) {
		return "", forbidden("file", "outside_roots")
	}
	if underAny(resolved, p.deniedPaths) {
		return "", forbidden("file", "denied")
	}
	if !isRegular(resolved) {
		return "", apperr.New(apperr.FileUnreadable, "field", "file")
	}
	return resolved, nil
}

func forbidden(field, reason string) *apperr.Error {
	return apperr.New(apperr.ForbiddenPath, "field", field, "reason", reason)
}

// checkPathSyntax は空・相対・制御文字（C0・DEL・C1・改行）を拒否する。shell が true なら "'" も拒否する。
func checkPathSyntax(field, path string, shell bool) error {
	bad := path == "" || !filepath.IsAbs(path) || strings.IndexFunc(path, unicode.IsControl) >= 0 ||
		(shell && strings.Contains(path, "'"))
	if bad {
		return apperr.New(apperr.InvalidArgs, "field", field)
	}
	return nil
}

// underAny は p が roots のどれかと一致するか、その配下にあるか。roots はそのつど
// シンボリックリンクを解決する（設定の読み込み後に作られたリンク・フォルダにも効かせる）。
func underAny(p string, roots []string) bool {
	for _, r := range roots {
		if r = resolveRoot(r); under(p, r) {
			return true
		}
	}
	return false
}

func resolveRoot(r string) string {
	if res, err := filepath.EvalSymlinks(r); err == nil {
		return filepath.Clean(res)
	}
	return filepath.Clean(r)
}

// under は p が root と一致するか root の配下か（"/mnt/nvme2" は "/mnt/nvme" の配下ではない）。
func under(p, root string) bool {
	if root == string(filepath.Separator) {
		return true
	}
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}
