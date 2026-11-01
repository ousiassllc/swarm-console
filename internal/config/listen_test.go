package config

import (
	"strings"
	"testing"
)

func TestCheckListenAddr(t *testing.T) {
	ok := []struct{ in, want string }{
		{"100.64.0.1:48120", "100.64.0.1:48120"},
		{"100.127.255.254:48120", "100.127.255.254:48120"},
		{"127.0.0.1:48120", "127.0.0.1:48120"},
		{"127.0.0.1:0", "127.0.0.1:0"}, // テスト用の空きポート
		{"[::1]:48120", "[::1]:48120"},
		{"[fd7a:115c:a1e0::1]:48120", "[fd7a:115c:a1e0::1]:48120"},
		{"[fd7a:115c:a1e0:ab12:4843:cd96:625d:1234]:1", "[fd7a:115c:a1e0:ab12:4843:cd96:625d:1234]:1"},
		{"[::ffff:100.64.0.1]:48120", "100.64.0.1:48120"},
	}
	for _, tt := range ok {
		got, err := CheckListenAddr(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("%q → %q, %v（%q のはず）", tt.in, got, err, tt.want)
		}
	}
	bad := []struct{ in, why string }{
		{"", "必須"},
		{"0.0.0.0:48120", "0.0.0.0"},
		{"[::]:48120", "0.0.0.0"},
		{"[::ffff:0.0.0.0]:48120", "0.0.0.0"},
		{":48120", "ホスト名"},
		{"*:48120", "ホスト名"},
		{"localhost:48120", "ホスト名"},
		{"example.com:48120", "ホスト名"},
		{"100.64.0.1", "ポート無し"},
		{"[fd7a:115c:a1e0::1]", "ポート無し"},
		{"::", "ポート無し"},
		{"100.64.0.1:", "ポート無し"},
		{"100.64.0.1:abc", "ポート"},
		{"100.64.0.1:65536", "ポート"},
		{"100.64.0.1:0", "ポート"},
		{"100.63.255.255:48120", "tailnet"},
		{"100.128.0.0:48120", "tailnet"},
		{"192.168.1.1:48120", "tailnet"},
		{"10.0.0.1:48120", "tailnet"},
		{"8.8.8.8:48120", "tailnet"},
		{"[fd7a:115c:a1e1::1]:48120", "tailnet"},
		{"[fe80::1]:48120", "tailnet"},
		{"[fe80::1%eth0]:48120", "ゾーン"},
		{" 100.64.0.1:48120", "ホスト名"},
		{"http://100.64.0.1:48120", "ホスト名"},
	}
	for _, tt := range bad {
		if got, err := CheckListenAddr(tt.in); err == nil {
			t.Errorf("%q は拒否のはずが %q", tt.in, got)
		}
	}
	// 理由の代表（メッセージが違う拒否理由を指していること）。
	if _, err := CheckListenAddr("0.0.0.0:1"); err == nil || !strings.Contains(err.Error(), "0.0.0.0") {
		t.Errorf("0.0.0.0 の理由: %v", err)
	}
	if _, err := CheckListenAddr("192.168.1.1:1"); err == nil || !strings.Contains(err.Error(), "tailnet") {
		t.Errorf("範囲外の理由: %v", err)
	}
}

func TestPaneNames(t *testing.T) {
	sessions := []string{"0", "work"}
	good := map[string]string{"0:0.1": "main", "work:1.2": "Docs-2", "0:0.0": "R1", "0:0.3": "T", "0:0.4": "B"}
	if got, err := checkPaneNames(good, sessions); err != nil || len(got) != 5 {
		t.Fatalf("正常系: %v %v", got, err)
	}
	if got, err := checkPaneNames(nil, sessions); err != nil || got != nil {
		t.Fatalf("空: %v %v", got, err)
	}
	bad := []struct {
		name string
		in   map[string]string
	}{
		{"自動名 T1", map[string]string{"0:0.0": "T1"}},
		{"自動名 小文字 b2", map[string]string{"0:0.0": "b2"}},
		{"自動名 R12", map[string]string{"0:0.0": "R12"}},
		{"固定名どうし", map[string]string{"0:0.0": "main", "0:0.1": "main"}},
		{"固定名どうし（大文字小文字）", map[string]string{"0:0.0": "Main", "work:0.1": "mAIN"}},
		{"% 始まり", map[string]string{"0:0.0": "%1"}},
		{"数字始まり", map[string]string{"0:0.0": "1a"}},
		{"空", map[string]string{"0:0.0": ""}},
		{"空白入り", map[string]string{"0:0.0": "a b"}},
		{"コロン入り", map[string]string{"0:0.0": "a:b"}},
		{"日本語", map[string]string{"0:0.0": "メイン"}},
		{"キー形式", map[string]string{"0.0": "x"}},
		{"キーが ID", map[string]string{"%1": "x"}},
		{"未知のセッション", map[string]string{"nope:0.0": "x"}},
	}
	for _, tt := range bad {
		if _, err := checkPaneNames(tt.in, sessions); err == nil {
			t.Errorf("%s: 拒否のはず", tt.name)
		}
	}
}
