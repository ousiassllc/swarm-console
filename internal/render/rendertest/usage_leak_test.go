package rendertest

import (
	"encoding/json"
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
