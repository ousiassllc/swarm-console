package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/ousiassllc/swarm-console/internal/updatecheck"
)

// releaseBase は tag のダウンロード先（API 応答の assets[] は使わず、検証済みの tag から組み立てる。FR-UPD-13）。
func releaseBase(tag string) string {
	return "https://github.com/" + updatecheck.Owner + "/" + updatecheck.Repo + "/releases/download/" + tag + "/"
}

// assetName は tag のバイナリのアセット名（P-57）。
func assetName(tag string) string { return binName + "_" + tag + "_linux_amd64" }

// install は SHA256SUMS → バイナリ（.new）→ 検証 → .old → マーカー → rename を、この順に行う（FR-UPD-14・15）。
// どこで失敗しても本体は旧バイナリのままで、.new とマーカーは消す。成功したときだけ .new は無い（rename 済み）。
func (u *Updater) install(ctx context.Context, tag string) (err error) {
	asset := assetName(tag)
	sumsBody, err := download(ctx, u.client, releaseBase(tag)+"SHA256SUMS", maxSumsBytes)
	if err != nil {
		return err
	}
	sums, err := readLimited(ctx, sumsBody, maxSumsBytes)
	_ = sumsBody.Close()
	if err != nil {
		return err
	}
	want, ok := parseSums(string(sums), asset)
	if !ok {
		return fail(ReasonChecksum)
	}

	newPath := filepath.Join(u.dir, newName)
	markerPath := filepath.Join(u.dir, pendingName)
	var tx txn
	defer func() {
		if !tx.committed {
			// 失敗・中断のとき、.new と、この入れ替えが書いたマーカーを消す（本体は旧バイナリのまま）。
			// ファイル操作の差し替えには依らない。
			_ = os.Remove(newPath)
			if tx.markerWritten {
				_ = os.Remove(markerPath)
			}
		}
	}()

	if err := u.fetchBinary(ctx, releaseBase(tag)+asset, newPath, want); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return u.swap(tag, newPath, markerPath, &tx)
}

// fetchBinary はバイナリを newPath に書きながら SHA-256 を計算し、want と一致したら fsync して閉じる。
func (u *Updater) fetchBinary(ctx context.Context, url, newPath, want string) error {
	body, err := download(ctx, u.client, url, maxBinaryBytes)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	if err := u.o.FS.Remove(newPath); err != nil { // 既存の .new は消してから作る
		return fail(ReasonWrite)
	}
	f, err := u.o.FS.Create(newPath, 0o755)
	if err != nil {
		return fail(ReasonWrite)
	}
	h := sha256.New()
	n, err := copyLimited(ctx, io.MultiWriter(f, h), body, maxBinaryBytes)
	if err != nil {
		_ = f.Close()
		return err
	}
	if n == 0 || hex.EncodeToString(h.Sum(nil)) != want {
		_ = f.Close()
		return fail(ReasonChecksum)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fail(ReasonWrite)
	}
	if err := f.Close(); err != nil {
		return fail(ReasonWrite)
	}
	return nil
}

// copyLimited は src を dst へ limit バイトまで写す。超えたら download（中断）。
// 読む側の失敗は download、書く側の失敗は write に分ける。
func copyLimited(ctx context.Context, dst io.Writer, src io.Reader, limit int64) (int64, error) {
	buf := make([]byte, 32<<10)
	var total int64
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > limit {
				return total, fail(ReasonDownload)
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, fail(ReasonWrite)
			}
		}
		switch {
		case errors.Is(rerr, io.EOF):
			return total, nil
		case rerr != nil:
			return total, abortOr(ctx, ReasonDownload)
		}
	}
}

// swap は検証済みの .new を本体にする：.old（ハードリンク）→ マーカー → rename（FR-UPD-15 の (2)〜(4)）。
// 本体の名前は一度も消えない。tx は失敗時に何を掃除するかを呼び出し元へ伝える。
func (u *Updater) swap(tag, newPath, markerPath string, tx *txn) error {
	binPath := filepath.Join(u.dir, binName)
	oldPath := filepath.Join(u.dir, oldName)
	if err := u.o.FS.Remove(oldPath); err != nil {
		return fail(ReasonWrite)
	}
	if err := u.o.FS.Link(binPath, oldPath); err != nil {
		return fail(ReasonWrite)
	}
	// 書き込みは原子的（失敗しても書きかけの一時ファイルは WritePending が消し、前のマーカーはそのまま）。
	marker, err := json.Marshal(pending{From: u.o.Current, To: tag})
	if err != nil {
		return fail(ReasonWrite)
	}
	if err := u.o.FS.WriteFileAtomic(markerPath, marker, 0o600); err != nil {
		return fail(ReasonWrite)
	}
	tx.markerWritten = true
	if err := u.o.FS.Rename(newPath, binPath); err != nil {
		return fail(ReasonWrite)
	}
	tx.committed = true
	return nil
}

// txn は入れ替えの進み具合。失敗時の掃除に使う。
type txn struct {
	markerWritten bool // マーカーの書き込みを始めた（この入れ替えのマーカーがありうる）
	committed     bool // 本体の rename が済んだ（以後は掃除しない）
}
