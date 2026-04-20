package quest

import (
	"context"
	"testing"
)

func TestJournal_RoundTrip(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	// Post a quest to attach the journal entry to.
	q := mustPost(t, db, pid, PostParams{Subject: "the big fix"})

	// Write a journal entry.
	if err := Journal(ctx, db, pid, q.ID, "alice", "found the root cause in auth.go"); err != nil {
		t.Fatalf("Journal: %v", err)
	}

	// Verify via Scroll that the note appears.
	res, err := Scroll(ctx, db, pid, q.ID)
	if err != nil {
		t.Fatalf("Scroll: %v", err)
	}

	// Look for our note among all notes (spec notes also exist).
	found := false
	for _, n := range res.Notes {
		if n.Note == "found the root cause in auth.go" && n.AgentID == "alice" {
			found = true
		}
	}
	if !found {
		t.Errorf("journal entry not found in scroll notes; got %d notes", len(res.Notes))
		for _, n := range res.Notes {
			t.Logf("  note: agentID=%q note=%q", n.AgentID, n.Note)
		}
	}

	// Verify the event was emitted.
	foundEvent := false
	for _, e := range res.Events {
		if e.Event == EventNoted {
			foundEvent = true
		}
	}
	if !foundEvent {
		t.Errorf("'noted' event not found in scroll timeline")
	}
}

func TestJournal_NotFound(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	err := Journal(ctx, db, pid, "QUEST-999", "", "text")
	if err == nil {
		t.Fatal("expected error for missing quest")
	}
}

func TestJournal_EmptyText(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	q := mustPost(t, db, pid, PostParams{Subject: "fix bug"})

	err := Journal(ctx, db, pid, q.ID, "", "")
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestJournal_MultipleEntries(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	q := mustPost(t, db, pid, PostParams{Subject: "refactor"})

	texts := []string{"first note", "second note", "third note"}
	for _, text := range texts {
		if err := Journal(ctx, db, pid, q.ID, "agent", text); err != nil {
			t.Fatalf("Journal(%q): %v", text, err)
		}
	}

	res, err := Scroll(ctx, db, pid, q.ID)
	if err != nil {
		t.Fatalf("Scroll: %v", err)
	}

	// Count how many of our journal entries appear.
	count := 0
	for _, n := range res.Notes {
		for _, text := range texts {
			if n.Note == text {
				count++
			}
		}
	}
	if count != 3 {
		t.Errorf("expected 3 journal entries in scroll, found %d", count)
	}
}
