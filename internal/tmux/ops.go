package tmux

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

var (
	paneIDRe  = regexp.MustCompile(`^%[0-9]+$`)
	keyNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,31}$`)
)

// pasteCounter は Paste のバッファ名を一意にする連番。
type pasteCounter struct{ n atomic.Uint64 }

// checkPane は tmux の -t に渡す ID（%12）の形だけを許す（フラグ・位置指定の混入防止）。
func checkPane(id string) error {
	if !paneIDRe.MatchString(id) {
		return apperr.New(apperr.InvalidArgs, "field", "pane_id")
	}
	return nil
}

// Capture は画面の最後から lines 行を返す（capture-pane -p -S -<lines>、ANSI なし・折り返し結合なし）。
// 履歴を含む最後の lines 行ではなく、末尾の空行（画面下の空き）を除いてから最後の lines 行を取る。
// 行は "\n" で連結し、末尾に改行は付けない。画面が空なら ""。
func (c *Client) Capture(ctx context.Context, id string, lines int) (string, error) {
	if err := checkPane(id); err != nil {
		return "", err
	}
	if lines < 1 {
		return "", apperr.New(apperr.InvalidArgs, "field", "lines")
	}
	out, err := c.run(ctx, subCapturePane, "", "-p", "-t", id, "-S", "-"+strconv.Itoa(lines))
	if err != nil {
		return "", err
	}
	ls := strings.Split(out, "\n")
	for len(ls) > 0 && strings.TrimSpace(ls[len(ls)-1]) == "" {
		ls = ls[:len(ls)-1]
	}
	if len(ls) > lines {
		ls = ls[len(ls)-lines:]
	}
	return strings.Join(ls, "\n"), nil
}

// SendKeys は tmux のキー名（Up・Enter・Escape・C-c・数字など）を send-keys する。
// キー名の形でないもの（空白・記号・先頭が "-"）は送らず invalid_args。
func (c *Client) SendKeys(ctx context.Context, id string, keys ...string) error {
	if err := checkPane(id); err != nil {
		return err
	}
	if len(keys) == 0 {
		return apperr.New(apperr.InvalidArgs, "field", "keys")
	}
	for _, k := range keys {
		if !keyNameRe.MatchString(k) {
			return apperr.New(apperr.InvalidArgs, "field", "keys")
		}
	}
	args := append([]string{"-t", id, "--"}, keys...)
	_, err := c.run(ctx, subSendKeys, "", args...)
	return err
}

// SendLiteral は send-keys -l で text をそのまま送る（キー名として解釈されない）。
func (c *Client) SendLiteral(ctx context.Context, id, text string) error {
	if err := checkPane(id); err != nil {
		return err
	}
	if text == "" {
		return nil
	}
	_, err := c.run(ctx, subSendKeys, "", "-l", "-t", id, "--", text)
	return err
}

// Paste は一意名のバッファへ標準入力から load-buffer し、paste-buffer -p -d で bracketed paste する。
// paste-buffer が失敗するとバッファ（text 全文）が tmux に残る。delete-buffer は許可サブコマンドに無いため消さない。
func (c *Client) Paste(ctx context.Context, id, text string) error {
	if err := checkPane(id); err != nil {
		return err
	}
	if text == "" {
		return apperr.New(apperr.InvalidArgs, "field", "text")
	}
	name, err := c.bufferName()
	if err != nil {
		return err
	}
	if _, err := c.run(ctx, subLoadBuffer, text, "-b", name, "-"); err != nil {
		return err
	}
	_, err = c.run(ctx, subPasteBuffer, "", "-p", "-d", "-b", name, "-t", id)
	return err
}

func (c *Client) bufferName() (string, error) {
	var r [4]byte
	if _, err := rand.Read(r[:]); err != nil {
		return "", apperr.Wrap(apperr.Internal, err)
	}
	return fmt.Sprintf("sc-%d-%d-%s", os.Getpid(), c.pasteID.n.Add(1), hex.EncodeToString(r[:])), nil
}

// SetTitle は select-pane -T でペインのタイトルを設定する。
func (c *Client) SetTitle(ctx context.Context, id, title string) error {
	if err := checkPane(id); err != nil {
		return err
	}
	_, err := c.run(ctx, subSelectPane, "", "-t", id, "-T", title)
	return err
}

// ServerStart は tmux サーバーの起動時刻（#{start_time}、epoch 秒の文字列）。サーバーが無ければ ""。
func (c *Client) ServerStart(ctx context.Context) (string, error) {
	out, err := c.run(ctx, subDisplay, "", "-p", "#{start_time}")
	if err != nil {
		if isErrNoServer(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Version は `tmux -V` の出力（例 "tmux 3.2a"）。
func (c *Client) Version(ctx context.Context) (string, error) {
	out, err := c.run(ctx, subVersion, "")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
