// Package template は `setup` が新規作成の出発点にするひな形（config.toml の見本）を持つ。
// 全キーの説明と既定値をコメントで並べてあり、`setup` を通さなくても手で使える。
package template

import _ "embed"

//go:embed config.toml.example
var configTOML []byte

// Bytes はひな形の内容（呼び出し側が書き換えても元には影響しない）。
func Bytes() []byte { return append([]byte(nil), configTOML...) }
