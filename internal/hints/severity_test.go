package hints

import "testing"

// TestSeverity_Rank locks the ordering used by the engine's budget
// pick: higher rank wins when multiple rules fire on one call.
func TestSeverity_Rank(t *testing.T) {
	want := []struct {
		sev  Severity
		rank int
	}{
		{SeverityBlocker, 4},
		{SeverityWarning, 3},
		{SeverityHint, 2},
		{SeverityFYI, 1},
	}
	for _, w := range want {
		if got := w.sev.Rank(); got != w.rank {
			t.Errorf("%s.Rank() = %d, want %d", w.sev, got, w.rank)
		}
	}
}

// TestSeverity_IsTop asserts the position-in-response policy.
func TestSeverity_IsTop(t *testing.T) {
	for sev, want := range map[Severity]bool{
		SeverityBlocker: true,
		SeverityWarning: true,
		SeverityHint:    false,
		SeverityFYI:     false,
	} {
		if got := sev.IsTop(); got != want {
			t.Errorf("%s.IsTop() = %t, want %t", sev, got, want)
		}
	}
}

// TestParseSeverity_Unknown asserts the defensive fallback on unknown
// DB strings.
func TestParseSeverity_Unknown(t *testing.T) {
	cases := []struct {
		in   string
		want Severity
	}{
		{"", SeverityHint},
		{"HINT", SeverityHint},
		{"  fyi  ", SeverityFYI},
		{"bogus", SeverityHint},
		{"blocker", SeverityBlocker},
	}
	for _, c := range cases {
		if got := ParseSeverity(c.in); got != c.want {
			t.Errorf("ParseSeverity(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

// TestResolveEraSeverity exercises the era gate on no-brief-24h's shape.
func TestResolveEraSeverity(t *testing.T) {
	// Empty per_era_severity → base severity unchanged.
	if got := ResolveEraSeverity(SeverityHint, EraMCP, ""); got != SeverityHint {
		t.Errorf("empty per_era: got %s, want hint", got)
	}
	// MCP stays hint, Bash demotes to fyi (the no-brief-24h shape).
	jsonBlob := `{"mcp":"hint","bash":"fyi"}`
	if got := ResolveEraSeverity(SeverityHint, EraMCP, jsonBlob); got != SeverityHint {
		t.Errorf("mcp era: got %s, want hint", got)
	}
	if got := ResolveEraSeverity(SeverityHint, EraBash, jsonBlob); got != SeverityFYI {
		t.Errorf("bash era: got %s, want fyi", got)
	}
	// Malformed JSON → fallback.
	if got := ResolveEraSeverity(SeverityHint, EraBash, "{not json"); got != SeverityHint {
		t.Errorf("malformed json: got %s, want hint (fallback)", got)
	}
}
