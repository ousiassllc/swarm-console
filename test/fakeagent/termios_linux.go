package main

import (
	"errors"
	"syscall"
	"unsafe"
)

// makeRaw は fd を生モード（ECHO と canonical を切り、CR を LF に変えない）にして、元に戻す関数を返す。
// tmux の send-keys は pty へ書くので、これで 1 キーずつ・そのままのバイトで読める。
// ISIG は残す（Ctrl+C で止められる）。fd が端末でなければ何もしない（単体テスト用）。
func makeRaw(fd int) (restore func(), err error) {
	var orig syscall.Termios
	if err := ioctl(fd, syscall.TCGETS, &orig); err != nil {
		if errors.Is(err, syscall.ENOTTY) {
			return func() {}, nil
		}
		return nil, err
	}
	raw := orig
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.IEXTEN
	raw.Cc[syscall.VMIN], raw.Cc[syscall.VTIME] = 1, 0
	if err := ioctl(fd, syscall.TCSETS, &raw); err != nil {
		return nil, err
	}
	return func() { _ = ioctl(fd, syscall.TCSETS, &orig) }, nil
}

func ioctl(fd int, req uintptr, t *syscall.Termios) error {
	//nolint:gosec // termios の取得・設定には ioctl に構造体のポインタを渡す必要がある（標準ライブラリの syscall のみ使う方針）
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(t))); e != 0 {
		return e
	}
	return nil
}
