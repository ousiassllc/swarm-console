package selfupdatetests

import (
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/selfupdate"
)

// 検証の失敗はすべて checksum で、入れ替えない（FR-UPD-14）。
func TestApplyChecksumFailures(t *testing.T) {
	asset := assetName(newVersion)
	h := sha(newContent)
	tests := []struct{ name, sums, binary string }{
		{"ハッシュ不一致", sha("other") + "  " + asset + "\n", newContent},
		{"行なし", sha(newContent) + "  other_file\n", newContent},
		{"SHA256SUMS が空", "", newContent},
		{"行が複数", h + "  " + asset + "\n" + h + "  " + asset + "\n", newContent},
		{"別ファイル名に付いただけの行は数えない", h + "  x" + asset + "\n", newContent},
		{"ハッシュが大文字", strings.ToUpper(h) + "  " + asset + "\n", newContent},
		{"ハッシュが短い", h[:63] + "  " + asset + "\n", newContent},
		{"ハッシュが 16 進でない", strings.Repeat("g", 64) + "  " + asset + "\n", newContent},
		{"区切りが空白 1 つ", h + " " + asset + "\n", newContent},
		{"区切りがタブ", h + "\t" + asset + "\n", newContent},
		{"区切りが空白 2 つ＋*", h + "  *" + asset + "\n", newContent},
		{"ハッシュなし", "  " + asset + "\n", newContent},
		{"空のバイナリ（ハッシュは正しい）", sha("") + "  " + asset + "\n", ""},
		{"ハッシュは合うが本文が途中で変わった", h + "  " + asset + "\n", newContent + "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newRig(t)
			g.serve(release{tag: newVersion, sums: body(tt.sums), binary: body(tt.binary)})
			err := g.apply(newVersion)
			wantFailure(t, err, selfupdate.ReasonChecksum)
			g.assertUntouched()
			g.wantApply(selfupdate.StateFailed, selfupdate.ReasonChecksum)
			if g.u.Status().At.IsZero() {
				t.Error("failed に at が付いていません")
			}
		})
	}
}

// sha256sum の出力（text・-b、LF・CRLF）は通る（FR-UPD-14）。
func TestApplyAcceptsSha256sumForms(t *testing.T) {
	asset := assetName(newVersion)
	h := sha(newContent)
	for name, sums := range map[string]string{
		"text":      h + "  " + asset + "\n",
		"binary":    h + " *" + asset + "\n",
		"CRLF":      h + "  " + asset + "\r\n",
		"binary+CR": h + " *" + asset + "\r\n",
		"末尾改行なし":    h + "  " + asset,
	} {
		t.Run(name, func(t *testing.T) {
			g := newRig(t)
			g.serve(release{tag: newVersion, sums: body(sums), binary: body(newContent)})
			if err := g.apply(newVersion); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// SHA256SUMS の中の別ファイルの行は読まない（複数アセットを並べても通る）。
func TestApplyAcceptsSumsWithOtherLines(t *testing.T) {
	g := newRig(t)
	other := sha("x") + "  swarm-console_v0.1.4_linux_amd64\n"
	g.serve(release{tag: newVersion, sums: body(other + sumsLine(newVersion, newContent)), binary: body(newContent)})
	if err := g.apply(newVersion); err != nil {
		t.Fatal(err)
	}
}
