package config

import "testing"

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
