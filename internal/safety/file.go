package safety

import (
	"io"
	"os"
	"syscall"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/apperr"
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

	// O_NONBLOCK: 検証後に FIFO などへ差し替えられても open で固まらない。O_NOFOLLOW: 検証後のリンク差し替えを拒否。
	f, err := os.OpenFile(resolved, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0) //nolint:gosec // CheckFile 済みのパス
	if err != nil {
		return "", unreadable(err)
	}
	defer func() { _ = f.Close() }()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return "", apperr.New(apperr.FileUnreadable, "field", "file")
	}
	// 1 字は UTF-8 で最大 4 バイト。これを超えたら 8000 字を超えているので全部は読まない。
	const maxBytes = MaxTextRunes * utf8.UTFMax
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return "", unreadable(err)
	}
	if len(data) > maxBytes {
		return "", apperr.New(apperr.TextTooLong, "max", MaxTextRunes)
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
