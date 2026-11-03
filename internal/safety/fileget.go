package safety

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// secretNames は file_get が拡張子や置き場所によらず断るファイル名（小文字で比べる）。
var secretNames = map[string]bool{".env": true, "config.toml": true, "tokens.toml": true, "profiles.toml": true}

// IsSecretName は name（ファイル名だけ）が秘密を置く名前か。
// .env・.env.*・*.pem・*.key・id_*（id_rsa など）・swarm-console の設定ファイル（config.toml・tokens.toml・profiles.toml）。
func IsSecretName(name string) bool {
	n := strings.ToLower(name)
	return secretNames[n] || strings.HasPrefix(n, ".env.") || strings.HasPrefix(n, "id_") ||
		strings.HasSuffix(n, ".pem") || strings.HasSuffix(n, ".key")
}

// IsBroadCWD は、ペインの cwd を file_get の根にしてはいけないか。
// cwd の実パス（シンボリックリンク解決後）が "/" かホームディレクトリ（home。実パスと解決前の値の両方）と完全に一致するか、
// cwd を実パスにできないとき true。ホームの下の作業フォルダやホームの親は false。home が空なら "/" だけを除く。
// 設定の file_get_roots やレポート置き場に明示した根には使わない（明示はそのまま許可）。
func IsBroadCWD(cwd, home string) bool {
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return true
	}
	resolved = filepath.Clean(resolved)
	if resolved == string(filepath.Separator) {
		return true
	}
	if home == "" {
		return false
	}
	if resolved == filepath.Clean(home) {
		return true
	}
	h, err := filepath.EvalSymlinks(home)
	return err == nil && resolved == filepath.Clean(h)
}

// CheckFileGet は file_get の path を検証し、シンボリックリンク解決後の絶対パスを返す。roots は取得を許す根。
//   - 空・制御文字 → invalid_args
//   - 相対パス・".." を含む・roots の外・denied_paths の一致・配下・秘密のファイル名（解決前・解決後の両方）・
//     tokens・profiles・notes の実パスと一致 → path_forbidden
//   - roots の下だが存在しない → not_found（roots の外は存在の有無によらず path_forbidden で、存在を漏らさない）
//
// 存在しないパスは、存在する一番深い祖先を解決してから判定する（途中のリンクで外へ抜ける未作成のパスを not_found にしない）。
func (p *Policy) CheckFileGet(path string, roots []string) (string, error) {
	if path == "" || strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return "", apperr.New(apperr.InvalidArgs, "field", "path")
	}
	if !filepath.IsAbs(path) {
		return "", pathForbidden("not_absolute")
	}
	if hasDotDot(path) {
		return "", pathForbidden("dotdot")
	}
	if IsSecretName(filepath.Base(path)) {
		return "", pathForbidden("secret")
	}
	resolved, exists := resolveLoose(path)
	if !underAny(resolved, roots) {
		return "", pathForbidden("outside_roots")
	}
	if underAny(resolved, p.deniedPaths) {
		return "", pathForbidden("denied")
	}
	if IsSecretName(filepath.Base(resolved)) || p.isSecretPath(resolved) {
		return "", pathForbidden("secret")
	}
	if !exists {
		return "", apperr.New(apperr.NotFound, "field", "path")
	}
	return resolved, nil
}

// isSecretPath は resolved が tokens・profiles・notes のファイルそのものか（名前が普通でも断る）。
func (p *Policy) isSecretPath(resolved string) bool {
	for _, s := range p.secretPaths {
		if resolveRoot(s) == resolved {
			return true
		}
	}
	return false
}

func pathForbidden(reason string) *apperr.Error {
	return apperr.New(apperr.PathForbidden, "field", "path", "reason", reason)
}

func hasDotDot(path string) bool {
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if part == ".." {
			return true
		}
	}
	return false
}

// resolveLoose は path のシンボリックリンクを解決する。存在しなければ、存在する一番深い祖先を解決して残りをつなぐ（exists=false）。
func resolveLoose(path string) (resolved string, exists bool) {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(r), true
	}
	rest, cur := filepath.Base(path), filepath.Dir(path)
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest), false
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return filepath.Clean(path), false
		}
		rest, cur = filepath.Join(filepath.Base(cur), rest), parent
	}
}
