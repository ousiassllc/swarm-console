package auth

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

const (
	helperEnv   = "AUTH_LOCK_TEST_HELPER" // "add" か "revoke"。立っているときだけ TestHelperProcess が働く
	helperPath  = "AUTH_LOCK_TEST_PATH"
	helperNames = "AUTH_LOCK_TEST_NAMES" // add: 接頭辞、revoke: 失効する名前の接頭辞
	helperCount = "AUTH_LOCK_TEST_COUNT"
)

// TestHelperProcess は別プロセスとして Add / Revoke を繰り返す（呼び出しは startHelper だけ）。
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		t.Skip("別プロセスとして起動されたときだけ動く")
	}
	path, prefix := os.Getenv(helperPath), os.Getenv(helperNames)
	n, err := strconv.Atoi(os.Getenv(helperCount))
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		name := fmt.Sprintf("%s%d", prefix, i)
		if mode == "add" {
			_, err = Add(path, name, ScopeReport, baseTime)
		} else {
			err = Revoke(path, name, baseTime)
		}
		if err != nil {
			t.Fatalf("%s %s: %v", mode, name, err)
		}
	}
}

// startHelper は このテストバイナリを別プロセスで起動する。待つのは wait を呼んだとき。
func startHelper(t *testing.T, mode, path, prefix string, n int) (wait func()) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestHelperProcess$", "-test.v") //nolint:gosec // 自分自身のテストバイナリ
	cmd.Env = append(os.Environ(),
		helperEnv+"="+mode, helperPath+"="+path, helperNames+"="+prefix, helperCount+"="+strconv.Itoa(n))
	var out safeBuffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := cmd.Wait(); err != nil {
			t.Fatalf("%s の別プロセスが失敗: %v\n%s", mode, err, out.String())
		}
	}
}

type safeBuffer struct{ b []byte }

func (s *safeBuffer) Write(p []byte) (int, error) { s.b = append(s.b, p...); return len(p), nil }
func (s *safeBuffer) String() string              { return string(s.b) }

func listNames(t *testing.T, path string) map[string]Info {
	t.Helper()
	infos, err := List(path)
	if err != nil {
		t.Fatal(err)
	}
	m := make(map[string]Info, len(infos))
	for _, in := range infos {
		m[in.Name] = in
	}
	return m
}

// 別プロセス 2 つと自プロセスが同数ずつ Add を並行に呼んでも、全部のトークンが残る（lost update しない）。
func TestAddAcrossProcessesKeepsAllTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf", "tokens.toml")
	const n = 15
	waitA := startHelper(t, "add", path, "pa", n)
	waitB := startHelper(t, "add", path, "pb", n)
	for i := range n {
		if _, err := Add(path, fmt.Sprintf("pc%d", i), ScopeReport, baseTime); err != nil {
			t.Fatal(err)
		}
	}
	waitA()
	waitB()
	got := listNames(t, path)
	if len(got) != 3*n {
		t.Fatalf("トークン数 = %d, want %d", len(got), 3*n)
	}
	for _, p := range []string{"pa", "pb", "pc"} {
		for i := range n {
			if _, ok := got[fmt.Sprintf("%s%d", p, i)]; !ok {
				t.Fatalf("%s%d が消えた", p, i)
			}
		}
	}
}

// Revoke（別プロセス）と Add（自プロセスと別プロセス）が並行しても、Revoke した名前の revoked_at は消えない。
func TestRevokeAndAddAcrossProcessesKeepRevokedAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf", "tokens.toml")
	const n = 15
	for i := range n {
		if _, err := Add(path, fmt.Sprintf("old%d", i), ScopeReport, baseTime); err != nil {
			t.Fatal(err)
		}
	}
	waitRevoke := startHelper(t, "revoke", path, "old", n)
	waitAdd := startHelper(t, "add", path, "pa", n)
	for i := range n {
		if _, err := Add(path, fmt.Sprintf("pc%d", i), ScopeReport, baseTime); err != nil {
			t.Fatal(err)
		}
	}
	waitRevoke()
	waitAdd()
	got := listNames(t, path)
	if len(got) != 3*n {
		t.Fatalf("トークン数 = %d, want %d", len(got), 3*n)
	}
	for i := range n {
		if got[fmt.Sprintf("old%d", i)].RevokedAt == "" {
			t.Fatalf("old%d の revoked_at が消えた", i)
		}
	}
}

// 別の fd がロックを持っている間、Add・Revoke・List は待ち、解かれたあとで進む。
func TestLockFileHeldByOtherFdBlocksUntilReleased(t *testing.T) {
	f := newFixture(t)
	other, err := os.OpenFile(f.path+lockSuffix, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	if err := syscall.Flock(int(other.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 3)
	go func() { _, err := Add(f.path, "late", ScopeReport, baseTime); done <- err }()
	go func() { done <- Revoke(f.path, "rep", baseTime) }()
	go func() { _, err := List(f.path); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("ロックを持たれているのに進んだ: %v", err)
	case <-time.After(300 * time.Millisecond): // 「進まない」ことを確かめる待ち（上限は 300 ms）
	}
	if err := syscall.Flock(int(other.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("ロックを解いたのに進まない")
		}
	}
	got := listNames(t, f.path)
	if _, ok := got["late"]; !ok || got["rep"].RevokedAt == "" {
		t.Fatalf("書き込みが残っていない: %+v", got)
	}
}

// ロックを取れなければ tokens.toml を書かずにエラーを返す。
func TestAddAndRevokeDoNotWriteWhenLockFails(t *testing.T) {
	f := newFixture(t)
	before := readFile(t, f.path)
	// ロックファイルの場所をディレクトリで塞ぐと open が失敗する
	if err := os.Remove(f.path + lockSuffix); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.path+lockSuffix, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(f.path, "late", ScopeReport, baseTime); err == nil {
		t.Fatal("ロックを取れないのに Add が成功した")
	}
	if err := Revoke(f.path, "rep", baseTime); err == nil {
		t.Fatal("ロックを取れないのに Revoke が成功した")
	}
	if after := readFile(t, f.path); after != before {
		t.Fatalf("ロックを取れないのに tokens.toml が変わった:\n%s", after)
	}
}

func TestLockFileIs0600BesideTokensAndMissingDirIsNotCreatedByReads(t *testing.T) {
	f := newFixture(t)
	fi, err := os.Stat(f.path + lockSuffix)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("ロックファイルの権限 = %v err=%v", fi, err)
	}
	missing := filepath.Join(t.TempDir(), "none", "tokens.toml")
	if infos, err := List(missing); err != nil || len(infos) != 0 {
		t.Fatalf("List = %v, %v", infos, err)
	}
	if err := Revoke(missing, "x", baseTime); err == nil {
		t.Fatal("無いトークンの Revoke が成功した")
	}
	if _, err := os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Fatalf("読むだけの操作がフォルダを作った: %v", err)
	}
}
