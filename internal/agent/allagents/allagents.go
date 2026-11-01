// Package allagents は組み込みの 3 アダプタ（omp・claude・codex）を登録するために blank import する。
// main とテストが `_ ".../internal/agent/allagents"` で読み込む（FR-AD-4）。
package allagents

import (
	_ "github.com/ousiassllc/swarm-console/internal/agent/claude" // アダプタの登録
	_ "github.com/ousiassllc/swarm-console/internal/agent/codex"  // アダプタの登録
	_ "github.com/ousiassllc/swarm-console/internal/agent/omp"    // アダプタの登録
)
