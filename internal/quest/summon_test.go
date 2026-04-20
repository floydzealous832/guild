package quest

import (
	"context"
	"testing"
)

func TestSummon_TransfersOwnership(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	// Post and accept as agentA.
	q := mustPost(t, db, pid, PostParams{Subject: "solve the hard problem"})
	if _, err := Accept(ctx, db, pid, q.ID, "agentA"); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	// Summon to agentB.
	if err := Summon(ctx, db, pid, q.ID, "agentB", "agentA"); err != nil {
		t.Fatalf("Summon: %v", err)
	}

	// Orders for agentB should include the quest.
	qB, err := Orders(ctx, db, pid, "agentB")
	if err != nil {
		t.Fatalf("Orders(agentB): %v", err)
	}
	found := false
	for _, oq := range qB {
		if oq.ID == q.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("quest not found in agentB's orders after summon")
	}

	// Orders for agentA should NOT include the quest.
	qA, err := Orders(ctx, db, pid, "agentA")
	if err != nil {
		t.Fatalf("Orders(agentA): %v", err)
	}
	for _, oq := range qA {
		if oq.ID == q.ID {
			t.Errorf("quest still found in agentA's orders after summon")
		}
	}

	// Scroll should show an "assigned" event.
	res, err := Scroll(ctx, db, pid, q.ID)
	if err != nil {
		t.Fatalf("Scroll: %v", err)
	}
	foundEvent := false
	for _, e := range res.Events {
		if e.Event == "assigned" && e.Data == "agentB" {
			foundEvent = true
		}
	}
	if !foundEvent {
		t.Error("'assigned' event with agentB payload not found in scroll")
	}
}

func TestSummon_NotFound(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	err := Summon(ctx, db, pid, "QUEST-999", "agentB", "")
	if err == nil {
		t.Fatal("expected error for missing quest")
	}
}

func TestSummon_EmptyTarget(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	q := mustPost(t, db, pid, PostParams{Subject: "test"})

	err := Summon(ctx, db, pid, q.ID, "", "")
	if err == nil {
		t.Fatal("expected error for empty target agent")
	}
}
