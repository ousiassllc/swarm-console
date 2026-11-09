package pair

import (
	"fmt"
	"image"
	"image/color"
	"regexp"
	"strings"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// decodeQR は端末に出た QR（上下 2 モジュールを 1 文字にした行）を画像に戻し、別の実装（gozxing）で読み取った中身を返す。
func decodeQR(t *testing.T, stderr string) string {
	t.Helper()
	var rows [][]rune
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, qrColor) {
			rows = append(rows, []rune(ansiRe.ReplaceAllString(line, "")))
		}
	}
	if len(rows) == 0 {
		t.Fatalf("標準エラーに QR がありません:\n%s", stderr)
	}
	const scale = 4 // 1 モジュール = 4 px
	w, h := len(rows[0]), len(rows)*2
	img := image.NewGray(image.Rect(0, 0, w*scale, h*scale))
	for y := range h {
		for x := range w {
			r := rows[y/2][x]
			dark := (y%2 == 0 && (r == '▀' || r == '█')) || (y%2 == 1 && (r == '▄' || r == '█'))
			c := color.Gray{Y: 255}
			if dark {
				c = color.Gray{Y: 0}
			}
			for dy := range scale {
				for dx := range scale {
					img.SetGray(x*scale+dx, y*scale+dy, c)
				}
			}
		}
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		t.Fatal(err)
	}
	res, err := qrcode.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		t.Fatalf("QR を読み取れません: %v\n%s", err, stderr)
	}
	return res.GetText()
}

// --qr：標準出力は従来どおり接続文字列 1 行で、標準エラーの QR の中身がそれと 1 文字も違わない（Issue #89）。
func TestPairQRMatchesConnectionString(t *testing.T) {
	for name, args := range map[string][]string{
		"既定":              {"--qr"},
		"sessions と name": {"--qr", "--sessions", "1,2", "--name", "my box 開発機"},
		"ops":             {"--qr", "--scope", "ops"},
		"name が最長":        {"--qr", "--name", strings.Repeat("あ", 100)},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, "100.64.0.5:48120", `["0", "1", "2"]`)
			stdout, stderr, err := e.pair(args...)
			if err != nil {
				t.Fatal(err)
			}
			wantPath := "/report"
			if args[len(args)-1] == "ops" {
				wantPath = "/ops"
			}
			_, plain := parseOutputPath(t, stdout, stderr, wantPath)
			if got, want := decodeQR(t, stderr), strings.TrimSuffix(stdout, "\n"); got != want {
				t.Errorf("QR の中身が接続文字列と違います:\n got  %s\n want %s", strings.ReplaceAll(got, plain, "<TOKEN>"), strings.ReplaceAll(want, plain, "<TOKEN>"))
			}
			if strings.Contains(stderr, plain) {
				t.Error("標準エラーに token の平文が文字として出ています")
			}
		})
	}
}

// --qr なしでは QR を出さない（標準エラーは案内だけ）。
func TestPairWithoutQRPrintsNoQR(t *testing.T) {
	e := newEnv(t, "100.64.0.5:48120", `["0"]`)
	_, stderr, err := e.pair()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr, "\x1b[") || strings.ContainsAny(stderr, "▀▄█") {
		t.Errorf("--qr なしなのに QR が出ています:\n%s", stderr)
	}
}

// 4,096 バイト以内でも QR に入りきらない大きさなら、--qr のときだけ token を発行せずに断る。--qr なしなら通る。
func TestPairQRTooLargeForQRIsRejectedBeforeIssuing(t *testing.T) {
	var names []string
	for i := range 70 {
		names = append(names, fmt.Sprintf(`"%s%02d"`, strings.Repeat("s", 28), i))
	}
	e := newEnv(t, "100.64.0.5:48120", "["+strings.Join(names, ",")+"]")
	name := strings.Repeat("あ", 100)

	stdout, stderr, err := e.pair("--qr", "--name", name)
	if err == nil || !strings.Contains(err.Error(), "QR") || stdout != "" {
		t.Fatalf("QR に入りきらないのに通った: err=%v stdout=%q", err, stdout)
	}
	if strings.Contains(stderr, "\x1b[") {
		t.Error("失敗したのに QR が出ています")
	}
	e.requireNoTokensFile(t)

	stdout, _, err = e.pair("--name", name)
	if err != nil || len(stdout) <= 2331 || len(stdout) > 4096 {
		t.Fatalf("--qr なしなら通るはず（%d バイト）: %v", len(stdout), err)
	}
}
