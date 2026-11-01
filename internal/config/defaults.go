package config

// 既定値のうち、`setup` が画面の初期値・例示に使うもの。Load と同じ値をここで 1 か所に持つ（コピーしない）。

// DefaultSessions は sessions の既定。
func DefaultSessions() []string { return []string{"0"} }

// DefaultAllowedRoots は allowed_roots の既定。
func DefaultAllowedRoots() []string { return []string{"/mnt/nvme/", "~/projects/"} }

// DefaultDeniedPaths は denied_paths の既定。config.toml に書くと入れ替わる（足されない。P-29）。
func DefaultDeniedPaths() []string { return []string{"~/dotfiles"} }
