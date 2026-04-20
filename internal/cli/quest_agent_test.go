package cli

import (
	"strings"
	"testing"
)

// runQuestAgent wraps runQuest but also resets agent flag state.
func runQuestAgent(t *testing.T, args []string) (stdout, stderr string, err error) {
	t.Helper()
	resetAgentFlagState()
	return runQuest(t, args)
}

func TestCLI_Journal_RoundTrip(t *testing.T) {
	setupQuestCLI(t, "agent-test")

	// Post a quest.
	_, _, err := runQuest(t, []string{"quest", "post", "--project", "agent-test", "the big task"})
	if err != nil {
		t.Fatalf("post: %v", err)
	}

	// Journal a note.
	stdout, _, err := runQuestAgent(t, []string{"quest", "journal",
		"--project", "agent-test",
		"QUEST-1", "found the bug in auth.go"})
	if err != nil {
		t.Fatalf("journal: %v", err)
	}
	// QUEST-45 unified CLI + MCP output: "journaled on QUEST-X"
	if !strings.Contains(stdout, "journaled on QUEST-1") {
		t.Errorf("journal stdout = %q, want 'journaled on QUEST-1'", stdout)
	}

	// Scroll should show the note.
	scrollOut, _, err := runQuestAgent(t, []string{"quest", "scroll",
		"--project", "agent-test", "QUEST-1"})
	if err != nil {
		t.Fatalf("scroll: %v", err)
	}
	if !strings.Contains(scrollOut, "found the bug in auth.go") {
		t.Errorf("scroll missing journal note; stdout = %q", scrollOut)
	}
}

func TestCLI_Campfire_RoundTrip(t *testing.T) {
	setupQuestCLI(t, "agent-test-cf")

	_, _, err := runQuest(t, []string{"quest", "post", "--project", "agent-test-cf", "deep task"})
	if err != nil {
		t.Fatalf("post: %v", err)
	}

	stdout, _, err := runQuestAgent(t, []string{"quest", "campfire",
		"--project", "agent-test-cf",
		"QUEST-1",
		"--hypothesis", "issue is in the cache",
		"--next", "try eviction logic"})
	if err != nil {
		t.Fatalf("campfire: %v", err)
	}
	if !strings.Contains(stdout, "campfire saved for QUEST-1") {
		t.Errorf("campfire stdout = %q, want 'campfire saved for QUEST-1'", stdout)
	}

	// Scroll should show the campfire checkpoint.
	scrollOut, _, err := runQuestAgent(t, []string{"quest", "scroll",
		"--project", "agent-test-cf", "QUEST-1"})
	if err != nil {
		t.Fatalf("scroll: %v", err)
	}
	if !strings.Contains(scrollOut, "cache") {
		t.Errorf("scroll missing campfire note; stdout = %q", scrollOut)
	}
}

func TestCLI_Bounties_NoTasks(t *testing.T) {
	setupQuestCLI(t, "empty-project")

	stdout, _, err := runQuestAgent(t, []string{"quest", "bounties",
		"--project", "empty-project"})
	if err != nil {
		t.Fatalf("bounties: %v", err)
	}
	if !strings.Contains(stdout, "no unclaimed tasks") {
		t.Errorf("bounties stdout = %q, want 'no unclaimed tasks'", stdout)
	}
}

func TestCLI_Bounties_WithTopTask(t *testing.T) {
	setupQuestCLI(t, "bounties-test")

	_, _, _ = runQuest(t, []string{"quest", "post", "--project", "bounties-test",
		"--priority", "P2", "low priority task"})
	_, _, _ = runQuest(t, []string{"quest", "post", "--project", "bounties-test",
		"--priority", "P0", "critical task"})

	stdout, _, err := runQuestAgent(t, []string{"quest", "bounties",
		"--project", "bounties-test"})
	if err != nil {
		t.Fatalf("bounties: %v", err)
	}
	// P0 quest should be at the top.
	if !strings.Contains(stdout, "critical task") {
		t.Errorf("bounties stdout missing P0 task; got %q", stdout)
	}
	if !strings.Contains(stdout, "quest accept") {
		t.Errorf("bounties stdout missing 'quest accept' prompt; got %q", stdout)
	}
}

func TestCLI_Bounties_BriefMode(t *testing.T) {
	setupQuestCLI(t, "brief-mode-test")

	// Write a brief first.
	_, _, err := runQuestAgent(t, []string{"quest", "brief",
		"--project", "brief-mode-test", "session ended well"})
	if err != nil {
		t.Fatalf("brief: %v", err)
	}

	// Post a task (should NOT appear in brief-only mode).
	_, _, _ = runQuest(t, []string{"quest", "post", "--project", "brief-mode-test", "task 1"})

	stdout, _, err := runQuestAgent(t, []string{"quest", "bounties",
		"--project", "brief-mode-test", "--brief"})
	if err != nil {
		t.Fatalf("bounties --brief: %v", err)
	}

	if !strings.Contains(stdout, "session ended well") {
		t.Errorf("brief-only mode missing briefing text; got %q", stdout)
	}
	// Should NOT contain 'quest accept' (no task surfacing in brief mode).
	if strings.Contains(stdout, "quest accept") {
		t.Errorf("brief-only mode should not surface tasks; got %q", stdout)
	}
}

func TestCLI_Brief_RoundTrip(t *testing.T) {
	setupQuestCLI(t, "brief-test")

	stdout, _, err := runQuestAgent(t, []string{"quest", "brief",
		"--project", "brief-test", "session wrap: all tests green"})
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	// QUEST-45 unified CLI + MCP output: "briefed for next session"
	if !strings.Contains(stdout, "briefed for next session") {
		t.Errorf("brief stdout = %q, want 'briefed for next session'", stdout)
	}

	// Next bounties call should show the brief.
	bOut, _, err := runQuestAgent(t, []string{"quest", "bounties",
		"--project", "brief-test"})
	if err != nil {
		t.Fatalf("bounties: %v", err)
	}
	if !strings.Contains(bOut, "all tests green") {
		t.Errorf("bounties missing brief text; got %q", bOut)
	}
}

func TestCLI_Summon_TransfersOwnership(t *testing.T) {
	setupQuestCLI(t, "summon-test")

	_, _, _ = runQuest(t, []string{"quest", "post", "--project", "summon-test", "delegation task"})
	_, _, _ = runQuest(t, []string{"quest", "accept", "--project", "summon-test",
		"--owner", "agentA", "QUEST-1"})

	stdout, _, err := runQuestAgent(t, []string{"quest", "summon",
		"--project", "summon-test",
		"--to", "agentB",
		"QUEST-1"})
	if err != nil {
		t.Fatalf("summon: %v", err)
	}
	if !strings.Contains(stdout, "agentB") {
		t.Errorf("summon stdout = %q, want agentB mention", stdout)
	}

	// Orders for agentB should include QUEST-1.
	oOut, _, err := runQuestAgent(t, []string{"quest", "orders",
		"--project", "summon-test", "--agent", "agentB"})
	if err != nil {
		t.Fatalf("orders agentB: %v", err)
	}
	if !strings.Contains(oOut, "QUEST-1") {
		t.Errorf("orders missing QUEST-1 for agentB; got %q", oOut)
	}

	// Orders for agentA should not include QUEST-1.
	aOut, _, err := runQuestAgent(t, []string{"quest", "orders",
		"--project", "summon-test", "--agent", "agentA"})
	if err != nil {
		t.Fatalf("orders agentA: %v", err)
	}
	if strings.Contains(aOut, "QUEST-1") {
		t.Errorf("orders for agentA should not include QUEST-1 after summon; got %q", aOut)
	}
}

func TestCLI_Scroll_ShowsTimeline(t *testing.T) {
	setupQuestCLI(t, "scroll-test")

	_, _, _ = runQuest(t, []string{"quest", "post", "--project", "scroll-test",
		"--priority", "P1", "scroll me"})
	_, _, _ = runQuestAgent(t, []string{"quest", "journal",
		"--project", "scroll-test", "QUEST-1", "a note"})

	stdout, _, err := runQuestAgent(t, []string{"quest", "scroll",
		"--project", "scroll-test", "QUEST-1"})
	if err != nil {
		t.Fatalf("scroll: %v", err)
	}
	if !strings.Contains(stdout, "QUEST-1") {
		t.Errorf("scroll missing QUEST-1; got %q", stdout)
	}
	if !strings.Contains(stdout, "NOTES") {
		t.Errorf("scroll missing NOTES section; got %q", stdout)
	}
	if !strings.Contains(stdout, "TIMELINE") {
		t.Errorf("scroll missing TIMELINE section; got %q", stdout)
	}
	if !strings.Contains(stdout, "a note") {
		t.Errorf("scroll missing journal note; got %q", stdout)
	}
}

func TestCLI_Orders_Empty(t *testing.T) {
	setupQuestCLI(t, "orders-empty-test")

	stdout, _, err := runQuestAgent(t, []string{"quest", "orders",
		"--project", "orders-empty-test", "--agent", "nobody"})
	if err != nil {
		t.Fatalf("orders: %v", err)
	}
	if !strings.Contains(stdout, "no tasks assigned") {
		t.Errorf("orders stdout = %q, want 'no tasks assigned'", stdout)
	}
}

func TestCLI_Bounties_ParallelismLine(t *testing.T) {
	setupQuestCLI(t, "parallel-test")

	// Post top quest with files=[a.go].
	_, _, _ = runQuest(t, []string{"quest", "post", "--project", "parallel-test",
		"--priority", "P0", "--files", "a.go", "top quest"})
	// Post parallel quest with files=[b.go] — no overlap.
	_, _, _ = runQuest(t, []string{"quest", "post", "--project", "parallel-test",
		"--priority", "P1", "--files", "b.go", "parallel quest"})

	stdout, _, err := runQuestAgent(t, []string{"quest", "bounties",
		"--project", "parallel-test"})
	if err != nil {
		t.Fatalf("bounties: %v", err)
	}
	if !strings.Contains(stdout, "can run in parallel") {
		t.Errorf("bounties missing parallelism line; got %q", stdout)
	}
}
