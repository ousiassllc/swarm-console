package reportvalues

import (
	"fmt"
	"strings"
	"testing"
)

// 実機の出力の形：accountsWithoutUsage[] の email はトップレベル（metadata なし）。reports[] は metadata.email。
// トップレベルの email から名前を作り（account-N にならない）、reason は without_usage。
// reports と同じ provider・メールのものは 1 つになり、reports 側が勝つ（reason は空）。
func TestParseUsageWithoutRealShape(t *testing.T) {
	u, err := ParseUsage(readFixture(t, "usage_without_real_shape.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Claude) != 2 {
		t.Fatalf("claude は 2 つ（重なる acct-a は 1 つ）: %+v", u.Claude)
	}
	if a := u.Claude[0]; a.Name != "acct-a" || a.Reason != "" || !a.SevenDay.Available || a.SevenDay.Used != 48.0 {
		t.Errorf("acct-a は reports 側の値で reason は空: %+v", a)
	}
	if b := u.Claude[1]; b.Name != "acct-b" || b.Reason != AccountWithoutUsage || b.FiveHour.Available || b.SevenDay.Available {
		t.Errorf("acct-b はトップレベルの email から名前を作り、without_usage: %+v", b)
	}
	if got := fmt.Sprintf("%+v", u); strings.Contains(got, "@") || strings.Contains(got, "dummy") {
		t.Errorf("結果にメール・ID の値が残っている: %s", got)
	}
}

// トップレベルの email が reports の同じ provider・メールと同じなら 1 つにする（reason は空）。
func TestParseUsageWithoutTopLevelEmailDedup(t *testing.T) {
	u, err := ParseUsage([]byte(`{"reports":[{"provider":"anthropic","metadata":{"email":"user@same.example"},"limits":[]}],
	 "accountsWithoutUsage":[{"provider":"anthropic","email":"user@same.example"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Claude) != 1 || u.Claude[0].Name != "same" || u.Claude[0].Reason != "" {
		t.Fatalf("1 つで reason は空: %+v", u.Claude)
	}
}

// トップレベルの email が無ければ metadata.email を読む（前の形）。両方あればトップレベルが勝つ。
func TestParseUsageWithoutMetadataEmailFallback(t *testing.T) {
	u, err := ParseUsage([]byte(`{"reports":[],"accountsWithoutUsage":[
	 {"provider":"anthropic","metadata":{"email":"user@meta.example"}},
	 {"provider":"anthropic","email":"user@top.example","metadata":{"email":"user@ignored.example"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Claude) != 2 || u.Claude[0].Name != "meta" || u.Claude[1].Name != "top" ||
		u.Claude[0].Reason != AccountWithoutUsage || u.Claude[1].Reason != AccountWithoutUsage {
		t.Fatalf("meta・top の順で without_usage: %+v", u.Claude)
	}
}
