// Package templates は report の既定テンプレートを go:embed で同梱する（FR-REP-5）。
// 設定フォルダ（~/.config/swarm-console/templates/）に同名のファイルがあれば、そちらが優先される。
package templates

import "embed"

// FS は report.ja.md・report.en.md を持つ。
//
//go:embed report.ja.md report.en.md
var FS embed.FS
