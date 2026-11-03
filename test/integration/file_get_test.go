package integration

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// file_get: 実際の HTTP（/ops）経由で、許可リスト（report_dir・file_get_roots）の中の画像が sha256 つきで取れ、
// 外・秘密・上限超え・種類違いは断られる。/report では呼べない。
func TestFileGet(t *testing.T) {
	extra := t.TempDir() // config の file_get_roots で足した根
	h := harness.Start(t, harness.Options{Config: map[string]string{
		"file_get_roots":     fmt.Sprintf("[%q]", extra),
		"file_get_max_bytes": "64",
	}})
	png := "\x89PNG\r\n\x1a\nintegration-body"
	inReports := filepath.Join(h.Reports, "T1", "shot.png")
	h.WriteFile(inReports, png)
	h.WriteFile(filepath.Join(h.Reports, ".env"), png)
	h.WriteFile(filepath.Join(h.Reports, "text.png"), "not an image")
	h.WriteFile(filepath.Join(h.Reports, "big.png"), png+string(make([]byte, 64)))
	outside := filepath.Join(h.Outside, "shot.png")
	h.WriteFile(outside, png)

	inExtra := filepath.Join(extra, "clip.png")
	h.WriteFile(inExtra, png)

	got := h.Ops().Map("file_get", map[string]any{"path": inReports})
	sum := sha256.Sum256([]byte(png))
	if got["sha256"] != hex.EncodeToString(sum[:]) || got["mime"] != "image/png" || got["size"] != float64(len(png)) || got["path"] != inReports {
		t.Fatalf("file_get = %v", got)
	}
	if r := h.Ops().Map("file_get", map[string]any{"path": inExtra}); r["sha256"] != got["sha256"] {
		t.Errorf("file_get_roots の下のファイル = %v", r)
	}
	if dec, err := base64.StdEncoding.DecodeString(got["data_base64"].(string)); err != nil || string(dec) != png {
		t.Errorf("data_base64 が元の中身と一致しません: %v", err)
	}

	for path, code := range map[string]string{
		outside:                                 "path_forbidden",
		filepath.Join(h.Reports, ".env"):        "path_forbidden",
		filepath.Join(h.Reports, "..", "x.png"): "path_forbidden",
		filepath.Join(h.Reports, "text.png"):    "unsupported_type",
		filepath.Join(h.Reports, "big.png"):     "too_large",
		filepath.Join(h.Reports, "none.png"):    "not_found",
	} {
		if r := h.Ops().Call("file_get", map[string]any{"path": path}); r.Code(t) != code {
			t.Errorf("%s: %s, want %s", path, r.Text, code)
		}
	}
	if r := h.Report().Call("file_get", map[string]any{"path": inReports}); r.Code(t) != "unknown_tool" {
		t.Errorf("/report の file_get: %s", r.Text)
	}
}
