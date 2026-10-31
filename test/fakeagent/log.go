package main

import (
	"fmt"
	"os"
)

// appendLog は出来事を --log のファイルへ追記する。形式は fakeagenttest.ReadLog が読む:
//
//	PASTE <バイト数>\n<本文>\n
//	CUSTOM <バイト数>\n<本文>\n
//	PICK <番号>\n
//	SUBMIT\n
//	ESCAPE\n
func appendLog(path string, e event) error {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // --log はテストが渡すパス
	if err != nil {
		return err
	}
	var werr error
	switch e.kind {
	case "PASTE", "CUSTOM":
		_, werr = fmt.Fprintf(f, "%s %d\n%s\n", e.kind, len(e.text), e.text)
	case "PICK":
		_, werr = fmt.Fprintf(f, "PICK %d\n", e.n)
	default:
		_, werr = fmt.Fprintf(f, "%s\n", e.kind)
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}
