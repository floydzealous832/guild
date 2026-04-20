package hints

import (
	"testing"
	"time"
)

// TestContext_RecordBumpsCallCount is the minimal sanity check.
func TestContext_RecordBumpsCallCount(t *testing.T) {
	c := NewContext("sess", EraMCP)
	if c.CallCount() != 0 {
		t.Fatalf("fresh CallCount = %d, want 0", c.CallCount())
	}
	c.RecordEvent(CallEvent{Tool: "lore_inscribe", Timestamp: time.Now()})
	c.RecordEvent(CallEvent{Tool: "quest_post", Timestamp: time.Now()})
	if c.CallCount() != 2 {
		t.Errorf("after 2 records CallCount = %d, want 2", c.CallCount())
	}
}

// TestContext_SeenSessionStart flips on guild_session_start or quest_bounties.
func TestContext_SeenSessionStart(t *testing.T) {
	c := NewContext("sess", EraMCP)
	if c.SeenSessionStart() {
		t.Fatal("fresh ctx should not have seen session start")
	}
	c.RecordEvent(CallEvent{Tool: "lore_inscribe"})
	if c.SeenSessionStart() {
		t.Error("inscribe should not flip seenSessionStart")
	}
	c.RecordEvent(CallEvent{Tool: "guild_session_start"})
	if !c.SeenSessionStart() {
		t.Error("guild_session_start did not flip seenSessionStart")
	}

	c2 := NewContext("sess2", EraMCP)
	c2.RecordEvent(CallEvent{Tool: "quest_bounties"})
	if !c2.SeenSessionStart() {
		t.Error("quest_bounties did not flip seenSessionStart")
	}
}

// TestContext_RecentlyCalled checks the contextual-suppression helper.
func TestContext_RecentlyCalled(t *testing.T) {
	c := NewContext("sess", EraMCP)
	for _, tool := range []string{"a", "b", "lore_appraise", "c", "d"} {
		c.RecordEvent(CallEvent{Tool: tool})
	}
	// The last event is excluded from the window.
	c.RecordEvent(CallEvent{Tool: "lore_inscribe"})

	// Look back 5 events → should include lore_appraise.
	if !c.RecentlyCalled(5, "lore_appraise") {
		t.Error("lore_appraise within 5 should be visible")
	}
	// Look back 2 events → should NOT include lore_appraise.
	if c.RecentlyCalled(2, "lore_appraise") {
		t.Error("lore_appraise outside window should not match")
	}
	// Empty names slice is a no-op.
	if c.RecentlyCalled(10) {
		t.Error("empty names should return false")
	}
}

// TestContext_Cooldown checks the per-rule cooldown accounting.
func TestContext_Cooldown(t *testing.T) {
	c := NewContext("sess", EraMCP)
	c.RecordEvent(CallEvent{Tool: "x"})
	c.MarkFired("rule-a", SeverityHint)
	// Same call → still "fired within" any cooldown > 0.
	if !c.RuleFiredWithin("rule-a", 5) {
		t.Error("cooldown check should be true immediately after MarkFired")
	}
	// Advance 5 calls → still within window of 10.
	for i := 0; i < 5; i++ {
		c.RecordEvent(CallEvent{Tool: "y"})
	}
	if !c.RuleFiredWithin("rule-a", 10) {
		t.Error("rule should still be in 10-call cooldown")
	}
	// Advance past the 5-call window.
	for i := 0; i < 10; i++ {
		c.RecordEvent(CallEvent{Tool: "z"})
	}
	if c.RuleFiredWithin("rule-a", 5) {
		t.Error("rule should have left its 5-call cooldown")
	}
}

// TestContext_FYICap asserts the per-session fyi counter increments only
// for fyi fires.
func TestContext_FYICap(t *testing.T) {
	c := NewContext("sess", EraMCP)
	c.MarkFired("rule-hint", SeverityHint)
	c.MarkFired("rule-fyi1", SeverityFYI)
	c.MarkFired("rule-fyi2", SeverityFYI)
	if c.FYIFiresThisSession() != 2 {
		t.Errorf("FYIFiresThisSession = %d, want 2", c.FYIFiresThisSession())
	}
}
