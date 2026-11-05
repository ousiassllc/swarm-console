package rendertest

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/reportvalues"
)

// omp の出力にメール・accountId・orgId があっても、ParseUsage → NewUsageJSON → JSON の流れで、アカウント名（ドメインの
// 最初のラベル・account-N）以外は出ない（FR-REP-18）。メールのユーザー名・ほかのドメインの部分・ID の値は一つも残らない。
func TestUsageJSONDoesNotLeakOmpIdentifiers(t *testing.T) {
	u, err := reportvalues.ParseUsage([]byte(`{"generatedAt":1793545357619,"reports":[
	 {"provider":"anthropic","metadata":{"email":"alice.user@secret.example","accountId":"acc-1234-leak","orgId":"org-5678-leak"},"limits":[
	  {"scope":{"shared":true},"window":{"id":"5h"},"amount":{"used":3,"unit":"percent"},"status":"ok"}]},
	 {"provider":"cursor","metadata":{"email":"bob.user@bad label.hidden.example","accountId":"acc-9999-leak","orgId":"org-0000-leak"},"limits":[]}],
	 "accountsWithoutUsage":[{"provider":"anthropic","metadata":{"email":"carol.user@lost.example","accountId":"acc-4321-leak","orgId":"org-8765-leak"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	snap := fixedSnapshot()
	snap.Usage = u
	b, err := json.Marshal(render.NewUsageJSON(snap))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	// 名前はドメインの最初のラベル（secret・lost）か account-N（ラベルが使えないメール）。
	for _, want := range []string{`"account":"secret"`, `"account":"lost"`, `"account":"account-1"`} {
		if !strings.Contains(got, want) {
			t.Errorf("%s がない: %s", want, got)
		}
	}
	for _, bad := range []string{"@", "user", "alice", "bob", "carol", "hidden", "bad label", ".example", "leak", "acc-", "org-"} {
		if strings.Contains(got, bad) {
			t.Errorf("JSON に %q が出ている: %s", bad, got)
		}
	}
}

// 実機の形（accountsWithoutUsage[] の email がトップレベル）でも、メール・accountId・orgId・orgName の値は出ない。
func TestUsageJSONDoesNotLeakRealShapeWithout(t *testing.T) {
	u, err := reportvalues.ParseUsage([]byte(`{"generatedAt":1793545357619,"reports":[
	 {"provider":"anthropic","metadata":{"email":"alice.user@secret.example","accountId":"acc-1234-leak","orgId":"org-5678-leak"},"limits":[]}],
	 "accountsWithoutUsage":[
	  {"provider":"anthropic","email":"carol.user@lost.example","accountId":"acc-4321-leak","orgId":"org-8765-leak","orgName":"Org Name Leak","type":"oauth"},
	  {"provider":"anthropic","email":"alice.user@secret.example","accountId":"acc-1111-leak","orgId":"org-2222-leak","orgName":"Org Name Leak","type":"oauth"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%+v", u); strings.Contains(got, "@") || strings.Contains(got, "leak") || strings.Contains(got, "user") {
		t.Errorf("Usage にメール・ID の値が出ている: %s", got)
	}
	snap := fixedSnapshot()
	snap.Usage = u
	b, err := json.Marshal(render.NewUsageJSON(snap))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	// secret は reports と重なるので 1 つ、lost は without_usage（account-N ではなくメールのラベルから名前を作る）。
	for _, want := range []string{`"account":"secret"`, `"account":"lost"`} {
		if strings.Count(got, want) != 1 {
			t.Errorf("%s が 1 つではない: %s", want, got)
		}
	}
	if strings.Contains(got, "account-") {
		t.Errorf("名前が account-N になっている: %s", got)
	}
	for _, bad := range []string{"@", "user", "alice", "carol", ".example", "leak", "acc-", "org-", "Org Name"} {
		if strings.Contains(got, bad) {
			t.Errorf("JSON に %q が出ている: %s", bad, got)
		}
	}
}
