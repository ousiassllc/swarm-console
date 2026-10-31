package mcpapi

import "testing"

func TestParseURI(t *testing.T) {
	for _, c := range []struct {
		uri, kind, pos string
		ok             bool
	}{
		{"swarm://report", "report", "", true},
		{"swarm://pane/T1", "pane", "T1", true},
		{"swarm://pane/%2512", "pane", "%12", true},
		{"swarm://notes/work:B2", "notes", "work:B2", true},
		{"swarm://notes/w1.T2", "notes", "w1.T2", true},
		{"swarm://pane/", "pane", "", false},
		{"swarm://pane/%zz", "pane", "", false},
		{"swarm://report/x", "", "", false},
		{"file:///etc/passwd", "", "", false},
	} {
		kind, pos, ok := parseURI(c.uri)
		if kind != c.kind || pos != c.pos || ok != c.ok {
			t.Errorf("parseURI(%q) = %q %q %v, want %q %q %v", c.uri, kind, pos, ok, c.kind, c.pos, c.ok)
		}
	}
}

func TestEscapePosRoundTrip(t *testing.T) {
	for _, pos := range []string{"%12", "T1", "work:B2", "w1.T2", "日本語"} {
		_, got, ok := parseURI(uriPanePfx + escapePos(pos))
		if !ok || got != pos {
			t.Errorf("%q → %q (ok=%v)", pos, got, ok)
		}
	}
}
