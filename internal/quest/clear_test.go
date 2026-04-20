package quest

import (
	"context"
	"errors"
	"testing"
)

// TestClear_MarksDone is the minimal happy path: clearing a status=next
// quest with no deps sets status=done and returns a zero-unblocked list.
func TestClear_MarksDone(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "lone"})

	res, err := Clear(context.Background(), db, pid, q.ID, "done and done")
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if res.Cleared.Status != StatusDone {
		t.Errorf("cleared.Status = %q, want done", res.Cleared.Status)
	}
	if len(res.Unblocked) != 0 {
		t.Errorf("unblocked = %v, want none", res.Unblocked)
	}
	if got := mustStatus(t, db, pid, q.ID); got != StatusDone {
		t.Errorf("DB status = %q, want done", got)
	}
}

// TestCascadeUnblock_Chain tests the A→B→C chain invariant called out in
// QUEST-9: clearing A unblocks B; clearing B unblocks C.
func TestCascadeUnblock_Chain(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	a := mustPost(t, db, pid, PostParams{Subject: "A"})
	b := mustPost(t, db, pid, PostParams{Subject: "B", DependsOn: []string{a.ID}})
	c := mustPost(t, db, pid, PostParams{Subject: "C", DependsOn: []string{b.ID}})

	if b.Status != StatusBlocked || c.Status != StatusBlocked {
		t.Fatalf("pre: b=%s c=%s, want both blocked", b.Status, c.Status)
	}

	// Clear A → B must flip to next, C stays blocked (dep B still not done).
	res, err := Clear(ctx, db, pid, a.ID, "")
	if err != nil {
		t.Fatalf("Clear A: %v", err)
	}
	if len(res.Unblocked) != 1 || res.Unblocked[0].ID != b.ID {
		t.Errorf("Clear A unblocked = %v, want [%s]", idsOf(res.Unblocked), b.ID)
	}
	if s := mustStatus(t, db, pid, b.ID); s != StatusNext {
		t.Errorf("B after clear A = %s, want next", s)
	}
	if s := mustStatus(t, db, pid, c.ID); s != StatusBlocked {
		t.Errorf("C after clear A = %s, want blocked", s)
	}

	// Clear B → C flips to next.
	res2, err := Clear(ctx, db, pid, b.ID, "")
	if err != nil {
		t.Fatalf("Clear B: %v", err)
	}
	if len(res2.Unblocked) != 1 || res2.Unblocked[0].ID != c.ID {
		t.Errorf("Clear B unblocked = %v, want [%s]", idsOf(res2.Unblocked), c.ID)
	}
	if s := mustStatus(t, db, pid, c.ID); s != StatusNext {
		t.Errorf("C after clear B = %s, want next", s)
	}

	t.Logf("CHAIN A→B→C: clear A unblocked [%s]; clear B unblocked [%s]",
		idsOf(res.Unblocked), idsOf(res2.Unblocked))
}

// TestCascadeUnblock_Diamond is the QUEST-9 diamond invariant:
//
//	  A
//	 / \
//	B   D
//	 \ /
//	  C
//
// Where B and D each depend on A, and C depends on BOTH B and D.
// Expected:
//  1. Clear A → unblocks B AND D in one transaction.
//  2. Clear B alone does NOT unblock C (D still not done).
//  3. Clear D → unblocks C (both deps now done).
func TestCascadeUnblock_Diamond(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	a := mustPost(t, db, pid, PostParams{Subject: "A"})
	b := mustPost(t, db, pid, PostParams{Subject: "B", DependsOn: []string{a.ID}})
	d := mustPost(t, db, pid, PostParams{Subject: "D", DependsOn: []string{a.ID}})
	c := mustPost(t, db, pid, PostParams{Subject: "C", DependsOn: []string{b.ID, d.ID}})

	if b.Status != StatusBlocked || d.Status != StatusBlocked || c.Status != StatusBlocked {
		t.Fatalf("pre: b=%s d=%s c=%s, want all blocked",
			b.Status, d.Status, c.Status)
	}

	// Step 1: clear A → unblock B + D.
	res1, err := Clear(ctx, db, pid, a.ID, "")
	if err != nil {
		t.Fatalf("Clear A: %v", err)
	}
	gotIDs := idsOf(res1.Unblocked)
	if len(res1.Unblocked) != 2 {
		t.Fatalf("Clear A unblocked %v, want 2 (B+D)", gotIDs)
	}
	unblockedSet := map[string]bool{}
	for _, q := range res1.Unblocked {
		unblockedSet[q.ID] = true
	}
	if !unblockedSet[b.ID] || !unblockedSet[d.ID] {
		t.Errorf("Clear A unblocked = %v, want set containing [%s,%s]",
			gotIDs, b.ID, d.ID)
	}
	if s := mustStatus(t, db, pid, c.ID); s != StatusBlocked {
		t.Errorf("C after clear A = %s, want blocked (D not done)", s)
	}

	// Step 2: clear B → C stays blocked (D still not done).
	res2, err := Clear(ctx, db, pid, b.ID, "")
	if err != nil {
		t.Fatalf("Clear B: %v", err)
	}
	if len(res2.Unblocked) != 0 {
		t.Errorf("Clear B unblocked = %v, want [] (D not done)", idsOf(res2.Unblocked))
	}
	if s := mustStatus(t, db, pid, c.ID); s != StatusBlocked {
		t.Errorf("C after clear B = %s, want still blocked", s)
	}

	// Step 3: clear D → C unblocks.
	res3, err := Clear(ctx, db, pid, d.ID, "")
	if err != nil {
		t.Fatalf("Clear D: %v", err)
	}
	if len(res3.Unblocked) != 1 || res3.Unblocked[0].ID != c.ID {
		t.Fatalf("Clear D unblocked = %v, want [%s]", idsOf(res3.Unblocked), c.ID)
	}
	if s := mustStatus(t, db, pid, c.ID); s != StatusNext {
		t.Errorf("C after clear D = %s, want next", s)
	}

	t.Logf("DIAMOND A→(B,D)→C: clear A unblocked %v (B+D); clear B unblocked %v (C stays blocked); clear D unblocked %v (C flips)",
		idsOf(res1.Unblocked), idsOf(res2.Unblocked), idsOf(res3.Unblocked))
}

// TestCascadeUnblock_MultiDep tests the 3-dep case: a quest with three
// upstream deps must stay blocked until ALL three are done.
func TestCascadeUnblock_MultiDep(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	x := mustPost(t, db, pid, PostParams{Subject: "X"})
	y := mustPost(t, db, pid, PostParams{Subject: "Y"})
	z := mustPost(t, db, pid, PostParams{Subject: "Z"})
	w := mustPost(t, db, pid, PostParams{
		Subject:   "W",
		DependsOn: []string{x.ID, y.ID, z.ID},
	})
	if w.Status != StatusBlocked {
		t.Fatalf("W.Status = %s, want blocked", w.Status)
	}

	// Clear X → W still blocked.
	if _, err := Clear(ctx, db, pid, x.ID, ""); err != nil {
		t.Fatalf("clear X: %v", err)
	}
	if s := mustStatus(t, db, pid, w.ID); s != StatusBlocked {
		t.Errorf("W after X = %s, want blocked", s)
	}

	// Clear Y → still blocked.
	if _, err := Clear(ctx, db, pid, y.ID, ""); err != nil {
		t.Fatalf("clear Y: %v", err)
	}
	if s := mustStatus(t, db, pid, w.ID); s != StatusBlocked {
		t.Errorf("W after Y = %s, want blocked", s)
	}

	// Clear Z → W flips.
	res, err := Clear(ctx, db, pid, z.ID, "")
	if err != nil {
		t.Fatalf("clear Z: %v", err)
	}
	if len(res.Unblocked) != 1 || res.Unblocked[0].ID != w.ID {
		t.Fatalf("unblocked = %v, want [%s]", idsOf(res.Unblocked), w.ID)
	}
	if s := mustStatus(t, db, pid, w.ID); s != StatusNext {
		t.Errorf("W after Z = %s, want next", s)
	}
}

// TestClear_NotFound returns ErrNotFound.
func TestClear_NotFound(t *testing.T) {
	db, pid := newTestDB(t)
	_, err := Clear(context.Background(), db, pid, "QUEST-404", "")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestClear_EmitsEvent sanity-checks that a `done` event lands with the
// report payload.
func TestClear_EmitsEvent(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "event-check"})
	if _, err := Clear(context.Background(), db, pid, q.ID, "commit abc123"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	var data string
	err := db.QueryRowContext(context.Background(),
		`SELECT data FROM task_events WHERE project_id = ? AND task_id = ? AND event = 'done'`,
		pid, q.ID,
	).Scan(&data)
	if err != nil {
		t.Fatalf("fetch event: %v", err)
	}
	if data != "commit abc123" {
		t.Errorf("event.data = %q, want %q", data, "commit abc123")
	}
}

// idsOf is a small helper for expressive test failure messages.
func idsOf(qs []*Quest) []string {
	ids := make([]string, 0, len(qs))
	for _, q := range qs {
		ids = append(ids, q.ID)
	}
	return ids
}
