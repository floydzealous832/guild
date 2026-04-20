package hints

import (
	"strings"
	"testing"
)

// TestTrigger_InscribeLooksLikeQuest is the table-driven spec for the
// TODO/should-fix phrase detector.
func TestTrigger_InscribeLooksLikeQuest(t *testing.T) {
	cases := []struct {
		name    string
		title   string
		summary string
		want    bool
	}{
		{"plain title", "wal pragma notes", "details", false},
		{"todo in title", "TODO migrate to WAL", "notes", true},
		{"todo in summary", "wal pragma notes", "TODO follow up on PRAGMA wait", true},
		{"need to phrase", "something", "we need to fix the migration order", true},
		{"should fix phrase", "bug", "should fix race in clear cmd", true},
		{"must fix phrase", "bug", "must fix flaky test asap", true},
		{"we should phrase", "rationale", "we should consider batching writes", true},
		{"empty both", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := CallEvent{
				Tool: "lore_inscribe",
				Args: map[string]any{
					"title":   c.title,
					"summary": c.summary,
				},
			}
			got := triggerInscribeLooksLikeQuest(nil, ev)
			if got != c.want {
				t.Errorf("got %t, want %t", got, c.want)
			}
		})
	}
}

// TestTrigger_NoSessionStart spans the bootstrap/non-bootstrap matrix.
func TestTrigger_NoSessionStart(t *testing.T) {
	// Fresh session — non-bootstrap call should trigger.
	c := NewContext("sess", EraMCP)
	if !triggerNoSessionStart(c, CallEvent{Tool: "lore_inscribe"}) {
		t.Error("expected trigger on fresh session")
	}

	// Bootstrap tools themselves do NOT self-trigger.
	if triggerNoSessionStart(c, CallEvent{Tool: "guild_session_start"}) {
		t.Error("trigger should not fire on guild_session_start itself")
	}
	if triggerNoSessionStart(c, CallEvent{Tool: "quest_bounties"}) {
		t.Error("trigger should not fire on quest_bounties itself")
	}

	// After seeing a session_start, subsequent calls are exempt.
	c.RecordEvent(CallEvent{Tool: "guild_session_start"})
	if triggerNoSessionStart(c, CallEvent{Tool: "lore_inscribe"}) {
		t.Error("trigger should suppress after session_start")
	}
}

// TestTrigger_SessionEndWithoutBrief verifies the 30-call threshold.
func TestTrigger_SessionEndWithoutBrief(t *testing.T) {
	c := NewContext("sess", EraMCP)
	// Below threshold.
	for i := 0; i < 10; i++ {
		c.RecordEvent(CallEvent{Tool: "x"})
	}
	if triggerSessionEndWithoutBrief(c, CallEvent{Tool: "y"}) {
		t.Error("should not trigger below call threshold")
	}
	// Reach threshold.
	for i := 0; i < 25; i++ {
		c.RecordEvent(CallEvent{Tool: "x"})
	}
	if !triggerSessionEndWithoutBrief(c, CallEvent{Tool: "y"}) {
		t.Error("should trigger above threshold with no brief")
	}
	// A brief in history suppresses.
	c.RecordEvent(CallEvent{Tool: "quest_brief"})
	if triggerSessionEndWithoutBrief(c, CallEvent{Tool: "y"}) {
		t.Error("should not trigger after a quest_brief was recorded")
	}
	// Self-suppression: quest_brief call itself does not trigger.
	c2 := NewContext("sess2", EraMCP)
	for i := 0; i < 40; i++ {
		c2.RecordEvent(CallEvent{Tool: "x"})
	}
	if triggerSessionEndWithoutBrief(c2, CallEvent{Tool: "quest_brief"}) {
		t.Error("quest_brief should not trigger itself")
	}
}

// TestTrigger_SlugQuery preserves the existing slug-regex behavior on
// top of the zero-result gate (QUEST-73).
func TestTrigger_SlugQuery(t *testing.T) {
	cases := []struct {
		q    string
		want bool
	}{
		{"", false},
		{"multi token query", false}, // whitespace disqualifies
		{"hyphen-slug-term", true},   // slug
		{"CamelCase", false},         // not slug
		{"QUEST-42", true},           // quest id
		{"quest-42", true},           // lowercase quest-42 is slug-matching
		{"simple", false},            // single token but no hyphen
	}
	for _, c := range cases {
		// Shape cases assume the zero-result signal is present. That is
		// what production plumbs for a miss; the regex check is the second
		// line of defense.
		got := triggerSlugQuery(nil, CallEvent{
			Args: map[string]any{
				"query":       c.q,
				zeroResultKey: true,
			},
		})
		if got != c.want {
			t.Errorf("q=%q zero=true: got %t, want %t", c.q, got, c.want)
		}
	}
}

// TestTrigger_SlugQuery_RequiresZeroResultSignal locks in the QUEST-73
// contract: even a slug-shaped query must NOT fire when the search
// succeeded or when the handler forgot to set the signal.
func TestTrigger_SlugQuery_RequiresZeroResultSignal(t *testing.T) {
	mk := func(args map[string]any) CallEvent {
		args["query"] = "QUEST-42"
		return CallEvent{Args: args}
	}
	// Signal absent — no fire (handler not wired; old behavior).
	if triggerSlugQuery(nil, mk(map[string]any{})) {
		t.Error("no zero-result key → should not fire")
	}
	// Signal present but false (hits returned) — no fire.
	if triggerSlugQuery(nil, mk(map[string]any{zeroResultKey: false})) {
		t.Error("zero-result=false → should not fire on successful search")
	}
	// Signal present and true — fire.
	if !triggerSlugQuery(nil, mk(map[string]any{zeroResultKey: true})) {
		t.Error("zero-result=true on slug-shaped query → should fire")
	}
}

// TestTrigger_JournalOutsideAccepted checks the session-scoped match.
func TestTrigger_JournalOutsideAccepted(t *testing.T) {
	c := NewContext("sess", EraMCP)
	// Journaling a quest with no prior accept → fire.
	if !triggerJournalOutsideAccepted(c, CallEvent{
		Args: map[string]any{"quest_id": "QUEST-10"},
	}) {
		t.Error("expected fire on journal without accept")
	}
	// Accept a different quest → still fire on our target.
	c.RecordEvent(CallEvent{Tool: "quest_accept",
		Args: map[string]any{"quest_id": "QUEST-99"}})
	if !triggerJournalOutsideAccepted(c, CallEvent{
		Args: map[string]any{"quest_id": "QUEST-10"},
	}) {
		t.Error("accept on unrelated quest should still fire")
	}
	// Accept the target → suppressed.
	c.RecordEvent(CallEvent{Tool: "quest_accept",
		Args: map[string]any{"quest_id": "QUEST-10"}})
	if triggerJournalOutsideAccepted(c, CallEvent{
		Args: map[string]any{"quest_id": "QUEST-10"},
	}) {
		t.Error("accept on target should suppress")
	}
	// Case-insensitive match.
	if triggerJournalOutsideAccepted(c, CallEvent{
		Args: map[string]any{"quest_id": "quest-10"},
	}) {
		t.Error("case mismatch should still suppress")
	}
}

// TestTrigger_NoBrief24h relies on the handler-stuffed Extras signal.
func TestTrigger_NoBrief24h(t *testing.T) {
	// Missing key → no fire.
	if triggerNoBrief24h(nil, CallEvent{}) {
		t.Error("unset signal should not fire")
	}
	// Key present, false → no fire.
	if triggerNoBrief24h(nil, CallEvent{
		Args: map[string]any{briefHintSessionKey: false},
	}) {
		t.Error("false signal should not fire")
	}
	// Key present, true → fire.
	if !triggerNoBrief24h(nil, CallEvent{
		Args: map[string]any{briefHintSessionKey: true},
	}) {
		t.Error("true signal should fire")
	}
}

// TestTrigger_InscribeWithoutAppraise verifies the 5-call window.
func TestTrigger_InscribeWithoutAppraise(t *testing.T) {
	c := NewContext("sess", EraMCP)
	// No prior appraise → fire.
	c.RecordEvent(CallEvent{Tool: "lore_inscribe"})
	if !triggerInscribeWithoutAppraise(c, CallEvent{Tool: "lore_inscribe"}) {
		t.Error("expected fire with no prior appraise")
	}
	// Recent appraise within window → suppress.
	c.RecordEvent(CallEvent{Tool: "lore_appraise"})
	c.RecordEvent(CallEvent{Tool: "lore_inscribe"})
	if triggerInscribeWithoutAppraise(c, CallEvent{Tool: "lore_inscribe"}) {
		t.Error("expected suppression with recent appraise")
	}
}

// TestTrigger_ClearWithoutReportDetail checks the 20-word boundary.
func TestTrigger_ClearWithoutReportDetail(t *testing.T) {
	short := "done"
	long := strings.Repeat("word ", 25)
	cases := []struct {
		report string
		want   bool
	}{
		{"", true},    // empty → trivially thin
		{short, true}, // 1 word
		{long, false}, // 25 words
	}
	for i, c := range cases {
		got := triggerClearWithoutReportDetail(nil, CallEvent{
			Args: map[string]any{"report": c.report},
		})
		if got != c.want {
			t.Errorf("case %d: got %t, want %t (words=%d)",
				i, got, c.want, wordCount(c.report))
		}
	}
}

// TestTrigger_PrincipleTooLong keys on kind AND wordcount.
func TestTrigger_PrincipleTooLong(t *testing.T) {
	long := strings.Repeat("word ", 70) // 70 words > principleMaxWords (60)
	short := "short oath"

	// Non-principle kind → never fire.
	if triggerPrincipleTooLong(nil, CallEvent{
		Args: map[string]any{"kind": "decision",
			"title": "t", "summary": long}}) {
		t.Error("non-principle should not fire")
	}

	// Principle + short → no fire.
	if triggerPrincipleTooLong(nil, CallEvent{
		Args: map[string]any{"kind": "principle",
			"title": short, "summary": short}}) {
		t.Error("short principle should not fire")
	}

	// Principle + long → fire.
	if !triggerPrincipleTooLong(nil, CallEvent{
		Args: map[string]any{"kind": "principle",
			"title": short, "summary": long}}) {
		t.Error("long principle should fire")
	}
}
