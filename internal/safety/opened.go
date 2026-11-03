package safety

import (
	"os"
	"strconv"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// CheckOpened は開いたファイル f の実パスが、CheckFileGet が返した resolved と同じか確かめる。
// O_NOFOLLOW は最後の成分のリンクしか拒否しないので、検証のあとに途中のディレクトリがリンクへ差し替えられても
// open は外のファイルを開いてしまう。開いた fd の実パスを /proc から読んで比べ、ずれていれば path_forbidden にする。
// /proc を読めないときは、確かめられないので断る（取得を許さない側に倒す）。
func CheckOpened(f *os.File, resolved string) error {
	actual, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(int(f.Fd())))
	if err != nil {
		return apperr.Wrap(apperr.Internal, err)
	}
	if actual != resolved {
		return pathForbidden("unreadable")
	}
	return nil
}
