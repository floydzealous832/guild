package quest

import (
	"context"
	"errors"
	"testing"
)

func TestForfeit_RestoresNext(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "cycle"})

	if _, err := Accept(context.Background(), db, pid, q.ID, "alice"); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if s := mustStatus(t, db, pid, q.ID); s != StatusInProgress {
		t.Fatalf("pre-forfeit status = %s", s)
	}

	got, err := Forfeit(context.Background(), db, pid, q.ID, "blocked on spec")
	if err != nil {
		t.Fatalf("Forfeit: %v", err)
	}
	if got.Status != StatusNext {
		t.Errorf("status = %q, want next", got.Status)
	}
	if got.Owner != "" {
		t.Errorf("owner = %q, want empty after forfeit", got.Owner)
	}

	// forfeit event landed.
	var events int
	err = db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM task_events WHERE project_id = ? AND task_id = ? AND event = 'released'`,
		pid, q.ID,
	).Scan(&events)
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 1 {
		t.Errorf("released events = %d, want 1", events)
	}
	// [released] note landed.
	var notes int
	err = db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM task_notes WHERE project_id = ? AND task_id = ? AND note LIKE '[released]%'`,
		pid, q.ID,
	).Scan(&notes)
	if err != nil {
		t.Fatalf("count notes: %v", err)
	}
	if notes != 1 {
		t.Errorf("[released] notes = %d, want 1", notes)
	}
}

func TestForfeit_NoNote(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "s"})
	_, _ = Accept(context.Background(), db, pid, q.ID, "a")
	if _, err := Forfeit(context.Background(), db, pid, q.ID, ""); err != nil {
		t.Fatalf("Forfeit: %v", err)
	}
	// No [released] note should exist.
	var notes int
	err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM task_notes WHERE project_id = ? AND task_id = ? AND note LIKE '[released]%'`,
		pid, q.ID,
	).Scan(&notes)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if notes != 0 {
		t.Errorf("note count = %d, want 0 (empty note)", notes)
	}
}

func TestForfeit_NotFound(t *testing.T) {
	db, pid := newTestDB(t)
	_, err := Forfeit(context.Background(), db, pid, "QUEST-404", "")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestForfeit_AcceptCycle verifies a post→accept→forfeit→accept round-trip.
func TestForfeit_AcceptCycle(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()
	q := mustPost(t, db, pid, PostParams{Subject: "cycle"})

	if _, err := Accept(ctx, db, pid, q.ID, "alice"); err != nil {
		t.Fatalf("accept1: %v", err)
	}
	if _, err := Forfeit(ctx, db, pid, q.ID, ""); err != nil {
		t.Fatalf("forfeit: %v", err)
	}
	// Second accept must succeed.
	got, err := Accept(ctx, db, pid, q.ID, "bob")
	if err != nil {
		t.Fatalf("accept2: %v", err)
	}
	if got.Owner != "bob" {
		t.Errorf("owner after re-accept = %q, want bob", got.Owner)
	}
}
