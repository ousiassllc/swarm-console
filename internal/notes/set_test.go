package notes

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

func TestSetMergeAndNullDelete(t *testing.T) {
	e := newEnv(t)
	e.set("%12", map[string]any{"from": "ふみ", "repo": "o/r", "items": []string{"#1", "#2"}, "memo": "m"})
	n := e.set("%12", map[string]any{"next": "次", "repo": nil, "items": []string{"#3"}})
	if n.From != "ふみ" || n.Memo != "m" || n.Next != "次" {
		t.Fatalf("他の項目が残っていない: %+v", n)
	}
	if n.Repo != "" || !reflect.DeepEqual(n.Items, []string{"#3"}) {
		t.Fatalf("null 削除・配列置換が効いていない: %+v", n)
	}
	if n.UpdatedAt != "2026-10-31T22:10:05+09:00" {
		t.Fatalf("updated_at = %q", n.UpdatedAt)
	}
	n = e.set("%12", map[string]any{"items": nil, "memo": nil})
	if n.Items != nil || n.Memo != "" {
		t.Fatalf("null で消えていない: %+v", n)
	}
	raw, _ := os.ReadFile(e.path)
	if strings.Contains(string(raw), `"memo"`) || strings.Contains(string(raw), `"items"`) {
		t.Fatalf("消した項目が JSON に残っている:\n%s", raw)
	}
}

func TestSetNestedObjects(t *testing.T) {
	e := newEnv(t)
	n := e.set("%1", map[string]any{
		"report_to": map[string]any{"kind": "agent", "name": "ふみ", "priority": true},
		"approval":  map[string]any{"text": "OK", "via": "ふみ", "at": "2026-10-31T19:35:00+09:00"},
	})
	if n.ReportTo == nil || !n.ReportTo.Priority || n.Approval == nil || n.Approval.Text != "OK" {
		t.Fatalf("%+v", n)
	}
	n = e.set("%1", map[string]any{"report_to": map[string]any{"kind": "email", "to": "a@example.com", "subject": "s"}, "approval": nil})
	if n.ReportTo.Kind != "email" || n.ReportTo.Name != "" || n.Approval != nil {
		t.Fatalf("report_to は置き換え、approval は消える: %+v", n)
	}
}

func TestSetRejectsInvalid(t *testing.T) {
	e := newEnv(t)
	e.set("%1", map[string]any{"from": "元"})
	before, _ := os.ReadFile(e.path)
	long := func(n int) string { return strings.Repeat("あ", n) }
	many := func(n int) []string { return make([]string, n) }
	cases := []struct {
		name  string
		set   map[string]any
		field string
	}{
		{"unknown key", map[string]any{"nope": "x"}, "nope"},
		{"updated_at", map[string]any{"updated_at": "2026-01-01T00:00:00Z"}, "updated_at"},
		{"log", map[string]any{"log": []any{}}, "log"},
		{"string type", map[string]any{"from": 1}, "from"},
		{"array type", map[string]any{"items": "x"}, "items"},
		{"array elem type", map[string]any{"rules": []any{1}}, "rules"},
		{"array elem null", map[string]any{"rules": []any{nil}}, "rules"},
		{"string too long", map[string]any{"branch": long(2001)}, "branch"},
		{"memo too long", map[string]any{"memo": long(4001)}, "memo"},
		{"array too many", map[string]any{"items": many(51)}, "items"},
		{"array elem too long", map[string]any{"items": []string{long(2001)}}, "items"},
		{"report_to kind", map[string]any{"report_to": map[string]any{"kind": "slack"}}, "report_to"},
		{"report_to agent no name", map[string]any{"report_to": map[string]any{"kind": "agent"}}, "report_to"},
		{"report_to email no @", map[string]any{"report_to": map[string]any{"kind": "email", "to": "x"}}, "report_to"},
		{"report_to unknown key", map[string]any{"report_to": map[string]any{"kind": "agent", "name": "a", "x": 1}}, "report_to"},
		{"report_to priority type", map[string]any{"report_to": map[string]any{"kind": "agent", "name": "a", "priority": "yes"}}, "report_to"},
		{"report_to not object", map[string]any{"report_to": "agent"}, "report_to"},
		{"approval no text", map[string]any{"approval": map[string]any{"via": "x"}}, "approval"},
		{"approval bad at", map[string]any{"approval": map[string]any{"text": "OK", "at": "昨日"}}, "approval"},
		{"approval text long", map[string]any{"approval": map[string]any{"text": long(2001)}}, "approval"},
		{"valid plus invalid", map[string]any{"next": "ok", "status": 3}, "status"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := e.s.Set(context.Background(), "%1", setOf(c.set))
			wantCode(t, err, apperr.InvalidArgs, c.field)
		})
	}
	after, _ := os.ReadFile(e.path)
	if string(before) != string(after) {
		t.Fatal("拒否したのにファイルが変わった")
	}
	if _, err := e.s.Set(context.Background(), "12", setOf(map[string]any{"from": "x"})); err == nil {
		t.Fatal("不正な pane ID を受け付けた")
	}
}

func TestSetBoundariesAccepted(t *testing.T) {
	e := newEnv(t)
	items := make([]string, 50)
	for i := range items {
		items[i] = strings.Repeat("字", 2000)
	}
	n := e.set("%1", map[string]any{"memo": strings.Repeat("字", 4000), "from": strings.Repeat("字", 2000), "items": items})
	if len(n.Items) != 50 {
		t.Fatal("上限ちょうどは受け付ける")
	}
}

func TestSetRejectsSecretWholly(t *testing.T) {
	secret := dummySecret()
	e := newEnv(t)
	e.set("%1", map[string]any{"from": "元"})
	before, _ := os.ReadFile(e.path)
	cases := map[string]map[string]any{
		"from":      {"from": secret},
		"items":     {"items": []string{"#1", "x " + secret}},
		"memo":      {"memo": "前 " + secret + " 後"},
		"report_to": {"report_to": map[string]any{"kind": "email", "to": "a@b.c", "subject": secret}},
		"approval":  {"approval": map[string]any{"text": secret}},
	}
	for field, set := range cases {
		t.Run(field, func(t *testing.T) {
			set["next"] = "同時に書く正常な値"
			_, err := e.s.Set(context.Background(), "%1", setOf(set))
			wantCode(t, err, apperr.SecretDetected, field)
			if strings.Contains(err.Error(), secret) || strings.Contains(apperr.As(err).Message(), secret) {
				t.Fatal("エラーに秘密の値が出た")
			}
			for _, v := range apperr.As(err).Details {
				if s, ok := v.(string); ok && strings.Contains(s, secret) {
					t.Fatal("details に秘密の値が出た")
				}
			}
		})
	}
	after, _ := os.ReadFile(e.path)
	if string(before) != string(after) {
		t.Fatal("部分書きまたは書き込みが起きた")
	}
	if n, _ := e.s.Get("%1"); n.Next != "" || n.From != "元" {
		t.Fatalf("メモが変わった: %+v", n)
	}
}

func TestLog(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for i := 1; i <= 60; i++ {
		size, err := e.s.Log(ctx, "%7", "行"+string(rune('A'+i%26))+strings.Repeat("x", i))
		if err != nil {
			t.Fatal(err)
		}
		if want := min(i, 50); size != want {
			t.Fatalf("i=%d size=%d want %d", i, size, want)
		}
	}
	n, ok := e.s.Get("%7")
	if !ok || len(n.Log) != 50 || !strings.HasSuffix(n.Log[0].Text, strings.Repeat("x", 11)) || !strings.HasSuffix(n.Log[49].Text, strings.Repeat("x", 60)) {
		t.Fatalf("古いものから落ちていない: len=%d", len(n.Log))
	}
	if n.Log[0].At != "2026-10-31T22:10:05+09:00" || n.UpdatedAt != n.Log[49].At {
		t.Fatalf("at/updated_at: %+v", n.Log[0])
	}
	for name, text := range map[string]string{"empty": "", "newline": "a\nb", "cr": "a\rb", "long": strings.Repeat("あ", 501)} {
		if _, err := e.s.Log(ctx, "%7", text); err == nil {
			t.Fatalf("%s を受け付けた", name)
		} else {
			wantCode(t, err, apperr.InvalidArgs, "text")
		}
	}
	if _, err := e.s.Log(ctx, "%7", strings.Repeat("あ", 500)); err != nil {
		t.Fatalf("500 字ちょうどは可: %v", err)
	}
	n, _ = e.s.Get("%7")
	_, err := e.s.Log(ctx, "%7", "token "+dummySecret())
	wantCode(t, err, apperr.SecretDetected, "text")
	if after, _ := e.s.Get("%7"); !reflect.DeepEqual(after, n) {
		t.Fatal("秘密拒否でログが変わった")
	}
}

func TestGetReturnsCopy(t *testing.T) {
	e := newEnv(t)
	n := e.set("%1", map[string]any{"items": []string{"#1"}})
	n.Items[0] = "改ざん"
	got, _ := e.s.Get("%1")
	got.Items[0] = "改ざん2"
	again, _ := e.s.Get("%1")
	if again.Items[0] != "#1" {
		t.Fatal("内部状態が外から書き換えられる")
	}
}
