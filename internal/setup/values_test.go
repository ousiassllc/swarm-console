package setup

import (
	"reflect"
	"strings"
	"testing"
)

// 入力中の検証は serve の起動時と同じ関数（FR-SET-5）。通らない値は次へ進めない。
func TestValidateListen(t *testing.T) {
	for _, ok := range []string{"100.64.0.1:48120", "127.0.0.1:48120", "[fd7a:115c:a1e0::1]:48120", " 127.0.0.1:1 "} {
		if err := ValidateListen(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for in, want := range map[string]string{
		"":                 "必須",
		"0.0.0.0:48120":    "0.0.0.0",
		"[::]:48120":       "::",
		"example.com:1234": "ホスト名",
		"100.64.0.1":       "ポート無し",
		"192.168.1.5:1":    "tailnet",
		"8.8.8.8:48120":    "tailnet",
	} {
		if err := ValidateListen(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", in, err, want)
		}
	}
}

func TestValidateSessions(t *testing.T) {
	if err := ValidateSessions("0, work"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", " , ", "a, a", "a.b", "a:b"} {
		if err := ValidateSessions(bad); err == nil {
			t.Errorf("%q は通らないはず", bad)
		}
	}
}

func TestValidatePaths(t *testing.T) {
	if err := ValidatePaths("allowed_roots", "/mnt/nvme/\n~/projects/\n\n", testHome, true); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePaths("denied_paths", "", testHome, false); err != nil {
		t.Fatalf("denied_paths は空でもよい: %v", err)
	}
	for _, bad := range []string{"relative/path", "~bob/x", "/a\x01b"} {
		if err := ValidatePaths("allowed_roots", bad, testHome, true); err == nil {
			t.Errorf("%q は通らないはず", bad)
		}
	}
	if err := ValidatePaths("allowed_roots", "", testHome, true); err == nil {
		t.Error("allowed_roots は 1 つ以上必要")
	}
}

func TestValidateAgentField(t *testing.T) {
	for _, tt := range []struct {
		field, in string
		ok        bool
	}{
		{"command", "", true}, {"command", "omp --x", true}, {"command", "a\nb", false},
		{"exit_command", "/quit", true}, {"auth_file", "~/x.json", true}, {"auth_file", "x.json", false},
	} {
		if err := ValidateAgentField("omp", tt.field, tt.in, testHome); (err == nil) != tt.ok {
			t.Errorf("%s %q: err = %v", tt.field, tt.in, err)
		}
	}
}

func TestParseInputs(t *testing.T) {
	if got := ParseSessions(" 0 ,work,, x "); !reflect.DeepEqual(got, []string{"0", "work", "x"}) {
		t.Fatalf("%v", got)
	}
	if got := ParsePaths("/a\r\n\n  ~/b  \n"); !reflect.DeepEqual(got, []string{"/a", "~/b"}) {
		t.Fatalf("%v", got)
	}
}

// 例示に出す組み込みの既定は、アダプタが持つ値（コピーしない）。
func TestAgentDefaults(t *testing.T) {
	if d := AgentDefaults("omp"); d.Command != "omp" || d.ExitCommand != "/exit" {
		t.Fatalf("%+v", d)
	}
	if d := AgentDefaults("codex"); d.Command != "codex" || d.ExitCommand == "" {
		t.Fatalf("%+v", d)
	}
}
