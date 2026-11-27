package safety

import (
	"io"
	"os"
	"syscall"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/config"
)

// ReadFile は CheckFile を通したファイルを UTF-8 として読み、中身を返す。
// 読めない・UTF-8 でない → file_unreadable。8000 字超 → text_too_long。制御文字は CheckText と同じ規則で
// forbidden_control_char（send_text にそのまま使えるように、ここで検証済みにする）。
func (p *Policy) ReadFile(path string) (string, error) {
	resolved, err := p.CheckFile(path)
	if err != nil {
		return "", err
	}
	unreadable := func(cause error) error { return apperr.Wrap(apperr.FileUnreadable, cause, "field", "file") }

	if p.beforeOpen != nil {
		p.beforeOpen(resolved)
	}
	// O_NONBLOCK: 検証後に FIFO などへ差し替えられても open で固まらない。O_NOFOLLOW: 検証後のリンク差し替えを拒否。
	f, err := os.OpenFile(resolved, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0) //nolint:gosec // CheckFile 済みのパス
	if err != nil {
		return "", unreadable(err)
	}
	defer func() { _ = f.Close() }()
	// O_NOFOLLOW は最後の成分しか拒否しない。開いた fd の実パスが検証したパスと違えば（途中のディレクトリの差し替え）断る。
	// send_text の語彙（forbidden_path）に直す。/proc を読めないときは internal のまま（確かめられないので断る）。
	if err := CheckOpened(f, resolved); err != nil {
		if ae := apperr.As(err); ae != nil && ae.Code == apperr.PathForbidden {
			return "", forbidden("file", "outside_roots")
		}
		return "", err
	}
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return "", apperr.New(apperr.FileUnreadable, "field", "file")
	}
	// 1 字は UTF-8 で最大 4 バイト。これを超えたら 8000 字を超えているので全部は読まない。
	const maxBytes = config.MaxTextRunes * utf8.UTFMax
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return "", unreadable(err)
	}
	if len(data) > maxBytes {
		return "", apperr.New(apperr.TextTooLong, "max", config.MaxTextRunes)
	}
	if !utf8.Valid(data) {
		return "", apperr.New(apperr.FileUnreadable, "field", "file")
	}
	text := string(data)
	if err := p.CheckText(text); err != nil {
		return "", err
	}
	return text, nil
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func isRegular(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}
