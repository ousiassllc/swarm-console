//go:build !linux

package main

import "errors"

// makeRaw は Linux 以外では使えない（tmux・/proc を前提にしたテスト用プログラムのため）。
func makeRaw(int) (func(), error) {
	return nil, errors.New("fakeagent は Linux でだけ動きます")
}
