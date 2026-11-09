package pair

import (
	"fmt"
	"io"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// QR は端末に出す。背景を白・前景を黒に明示して、端末の配色（暗い背景）に依らず「暗いモジュール＝黒」で読み取れるようにする。
// 上下 2 モジュールを 1 文字（▀ ▄ █ 空白）にまとめるので、正方形に近い見た目で行数が半分になる。
const (
	qrColor = "\x1b[30;47m" // 前景 黒・背景 白
	qrReset = "\x1b[0m"
	// qrQuiet は QR の外側の余白（モジュール数）。仕様の 4 モジュール。
	qrQuiet = 4
)

// qrCell は上下 2 モジュール（暗いなら true）に対応する 1 文字。
var qrCell = map[[2]bool]string{
	{false, false}: " ",
	{true, false}:  "▀",
	{false, true}:  "▄",
	{true, true}:   "█",
}

// renderQR は content の QR を端末向けの文字列（末尾の改行つき）にする。content が QR に入りきらないときはエラー。
func renderQR(content string) (string, error) {
	code, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", fmt.Errorf("接続文字列（%d バイト）を QR にできません（--name・--sessions を短くしてください）: %w", len(content), err)
	}
	code.DisableBorder = true // 余白は下で自分で足す
	bitmap := code.Bitmap()
	size := len(bitmap) + 2*qrQuiet
	dark := func(x, y int) bool {
		x, y = x-qrQuiet, y-qrQuiet
		return y >= 0 && y < len(bitmap) && x >= 0 && x < len(bitmap) && bitmap[y][x]
	}
	var b strings.Builder
	for y := 0; y < size; y += 2 {
		b.WriteString(qrColor)
		for x := range size {
			b.WriteString(qrCell[[2]bool{dark(x, y), dark(x, y+1)}])
		}
		b.WriteString(qrReset + "\n")
	}
	return b.String(), nil
}

// writeQR は接続文字列の QR を w（標準エラー）へ出す。token は発行済みなので、失敗したら失効の手順を添える。
func writeQR(content, tokenName string, w io.Writer) error {
	qr, err := renderQR(content)
	if err == nil {
		_, err = io.WriteString(w, "\nswarm-viewer の「QR を読み取る」で、次の QR を読み取ってください（QR にも token が入っています）。\n"+qr)
	}
	if err != nil {
		return fmt.Errorf("QR を出せませんでした。接続文字列は標準出力に出ています。使わないなら `swarm-console token revoke %s` で失効してください: %w", tokenName, err)
	}
	return nil
}
