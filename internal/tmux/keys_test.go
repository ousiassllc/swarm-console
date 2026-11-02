package tmux

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

const catPane = `stty -echo; printf 'READY\n'; exec cat`

func TestSendKeysAndLiteral(t *testing.T) {
	srv, c := setup(t)
	ctx := context.Background()
	id := srv.NewSession("keys", 80, 20, catPane)
	srv.WaitForText(id, "READY")

	// -l はキー名として解釈されない（"Enter" という文字列がそのまま入る）。先頭が "-" でも、シェル記号でも同じ。
	for _, lit := range []string{"Enter", "-x --y", "$HOME; echo `id` 'q' \"d\"", "日本語"} {
		if err := c.SendLiteral(ctx, id, lit); err != nil {
			t.Fatal(err)
		}
		if err := c.SendKeys(ctx, id, "Enter"); err != nil {
			t.Fatal(err)
		}
		srv.WaitFor(id, func(s string) bool { return strings.Count(s, lit) >= 1 })
	}
	// キー列: 数字キー・C-キー。C-d は cat を終わらせる。
	if err := c.SendKeys(ctx, id, "1", "2", "Enter"); err != nil {
		t.Fatal(err)
	}
	srv.WaitFor(id, func(s string) bool { return strings.Contains(s, "12") })
}

// 末尾が ";"・"\;" のテキストも、tmux に区切りと解釈されず ";" ごとペインへ入力される。
func TestSendLiteralKeepsTrailingSemicolon(t *testing.T) {
	srv, c := setup(t)
	ctx := context.Background()
	id := srv.NewSession("semi", 80, 20, catPane)
	srv.WaitForText(id, "READY")
	for _, lit := range []string{"echo hi;", `echo bye\;`} {
		if err := c.SendLiteral(ctx, id, lit); err != nil {
			t.Fatal(err)
		}
		if err := c.SendKeys(ctx, id, "Enter"); err != nil {
			t.Fatal(err)
		}
		// cat が行を打ち返す。入力は stty -echo で見えないので、行ぜんたいが一致する行を待つ。
		srv.WaitFor(id, func(s string) bool {
			for _, line := range strings.Split(s, "\n") {
				if strings.TrimRight(line, " ") == lit {
					return true
				}
			}
			return false
		})
	}
}

func TestEscapeTrailingSemicolon(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "abc": "abc", "a;b": "a;b", "abc;": `abc\;`, `abc\;`: `abc\\;`, ";": `\;`, "a;;": `a;\;`,
	} {
		if got := escapeTrailingSemicolon(in); got != want {
			t.Errorf("escapeTrailingSemicolon(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSendKeysRejectsBadNames(t *testing.T) {
	c := New("sctest-never-started")
	ctx := context.Background()
	for _, keys := range [][]string{nil, {""}, {"-l"}, {"a b"}, {"Enter; ls"}, {"Up", "$x"}, {"é"}} {
		if ae := apperr.As(c.SendKeys(ctx, "%1", keys...)); ae.Code != apperr.InvalidArgs {
			t.Errorf("SendKeys(%q): invalid_args のはずが %v", keys, ae)
		}
	}
	if ae := apperr.As(c.SendKeys(ctx, "bad", "Enter")); ae.Code != apperr.InvalidArgs {
		t.Errorf("不正な ID: %v", ae)
	}
	if ae := apperr.As(c.SendLiteral(ctx, "-t", "x")); ae.Code != apperr.InvalidArgs {
		t.Errorf("不正な ID: %v", ae)
	}
}

func TestPasteBracketed(t *testing.T) {
	srv, c := setup(t)
	ctx := context.Background()
	// ペイン側で bracketed paste モードを有効にして cat -v で見える形にする。
	id := srv.NewSession("bp", 100, 30, `stty -echo; printf '\033[?2004hREADY\n'; exec cat -v`)
	srv.WaitForText(id, "READY")

	if err := c.Paste(ctx, id, "hello 'quote' $HOME"); err != nil {
		t.Fatal(err)
	}
	if err := c.SendKeys(ctx, id, "Enter"); err != nil {
		t.Fatal(err)
	}
	srv.WaitForText(id, "^[[200~hello 'quote' $HOME^[[201~")

	if err := c.Paste(ctx, id, "line1\nline2"); err != nil {
		t.Fatal(err)
	}
	if err := c.SendKeys(ctx, id, "Enter"); err != nil {
		t.Fatal(err)
	}
	srv.WaitForText(id, "^[[200~line1\nline2^[[201~")

	// load-buffer の一時バッファは paste-buffer -d で消える。
	if got := srv.Run("list-buffers"); got != "" {
		t.Fatalf("バッファが残っている: %q", got)
	}
}

func TestPasteJapaneseAndLong(t *testing.T) {
	srv, c := setup(t)
	ctx := context.Background()
	id := srv.NewSession("jp", 120, 120, catPane)
	srv.WaitForText(id, "READY")

	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("%03d", i)+"行"+strings.Repeat("あ", i%7)+"-日本語とEnglishの混在 "+strings.Repeat("x", i%11+1))
	}
	text := strings.Join(lines, "\n")
	if err := c.Paste(ctx, id, text); err != nil {
		t.Fatal(err)
	}
	if err := c.SendKeys(ctx, id, "Enter"); err != nil {
		t.Fatal(err)
	}
	screen := srv.WaitFor(id, func(s string) bool { return strings.Contains(s, lines[99]) })
	if !strings.Contains(screen, text) {
		t.Fatalf("貼り付けた全文が画面に出ていない:\n%s", screen)
	}
}

// ctx が中断済みでも、本文は名前付きバッファとして残らない（#57）。
func TestPasteCancelledLeavesNoBuffer(t *testing.T) {
	srv, c := setup(t)
	id := srv.NewSession("pc", 80, 20, sleepCmd)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Paste(ctx, id, "secret body"); err == nil {
		t.Fatal("中断した ctx で Paste が成功した")
	}
	if got := srv.Run("list-buffers"); got != "" {
		t.Fatalf("バッファが残っている: %q", got)
	}
}

func TestPasteRejectsEmptyAndBadID(t *testing.T) {
	c := New("sctest-never-started")
	if ae := apperr.As(c.Paste(context.Background(), "%1", "")); ae.Code != apperr.InvalidArgs {
		t.Errorf("空文字: %v", ae)
	}
	if ae := apperr.As(c.Paste(context.Background(), "x", "a")); ae.Code != apperr.InvalidArgs {
		t.Errorf("不正な ID: %v", ae)
	}
}

func TestPasteFailureIsTmuxError(t *testing.T) {
	srv, c := setup(t)
	srv.NewSession("pf", 80, 20, sleepCmd)
	err := c.Paste(context.Background(), "%999", "text")
	assertTmuxError(t, err, "paste-buffer")
	// 貼り付けに失敗しても、本文が名前付きバッファとして tmux に残らない（#57）。
	if got := srv.Run("list-buffers"); got != "" {
		t.Fatalf("バッファが残っている: %q", got)
	}
	if strings.Contains(err.Error(), "text") && strings.Contains(apperr.As(err).Message(), "text") {
		t.Fatal("本文がメッセージに出ている")
	}
}
