package quest

import (
	"context"
	"errors"
	"testing"
)

func TestUpdate_ScalarOverwrite(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "s", Priority: "P2"})
	if _, err := Update(context.Background(), db, pid, q.ID, UpdateParams{Priority: "P0"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := mustLoad(t, db, pid, q.ID)
	if got.Priority != "P0" {
		t.Errorf("priority = %q, want P0 (not appended)", got.Priority)
	}
	// Subject unchanged.
	if got.Subject != "s" {
		t.Errorf("subject = %q, want s", got.Subject)
	}
}

func TestUpdate_ScalarMultipleOverwrites(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "s", Priority: "P2"})
	ctx := context.Background()
	if _, err := Update(ctx, db, pid, q.ID, UpdateParams{Priority: "P1"}); err != nil {
		t.Fatalf("1: %v", err)
	}
	if _, err := Update(ctx, db, pid, q.ID, UpdateParams{Priority: "P0"}); err != nil {
		t.Fatalf("2: %v", err)
	}
	got := mustLoad(t, db, pid, q.ID)
	if got.Priority != "P0" {
		t.Errorf("priority = %q, want P0 (last wins)", got.Priority)
	}
}

func TestUpdate_ListAppend_Files(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "s", Files: []string{"a.go"}})
	if _, err := Update(context.Background(), db, pid, q.ID, UpdateParams{Files: []string{"b.go"}}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := mustLoad(t, db, pid, q.ID)
	if len(got.Files) != 2 || got.Files[0] != "a.go" || got.Files[1] != "b.go" {
		t.Errorf("files = %v, want [a.go b.go]", got.Files)
	}
}

func TestUpdate_ListReplace_Files(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "s", Files: []string{"a.go"}})
	if _, err := Update(context.Background(), db, pid, q.ID, UpdateParams{
		ReplaceFiles: []string{"b.go"},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := mustLoad(t, db, pid, q.ID)
	if len(got.Files) != 1 || got.Files[0] != "b.go" {
		t.Errorf("files = %v, want [b.go]", got.Files)
	}
}

func TestUpdate_ListReplace_Acceptance(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{
		Subject:    "s",
		Acceptance: []string{"old1", "old2"},
	})
	if _, err := Update(context.Background(), db, pid, q.ID, UpdateParams{
		ReplaceAcceptance: []string{"new1", "new2", "new3"},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := mustLoad(t, db, pid, q.ID)
	if len(got.Acceptance) != 3 {
		t.Fatalf("acceptance = %v, want 3", got.Acceptance)
	}
	for i, want := range []string{"new1", "new2", "new3"} {
		if got.Acceptance[i] != want {
			t.Errorf("acc[%d] = %q, want %q", i, got.Acceptance[i], want)
		}
	}
}

func TestUpdate_ListAppend_Acceptance_PreservesCommas(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "s"})
	if _, err := Update(context.Background(), db, pid, q.ID, UpdateParams{
		Acceptance: []string{"foo, bar, baz", "one; two"},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := mustLoad(t, db, pid, q.ID)
	if len(got.Acceptance) != 2 {
		t.Fatalf("acceptance = %v, want 2", got.Acceptance)
	}
	if got.Acceptance[0] != "foo, bar, baz" {
		t.Errorf("acc[0] = %q", got.Acceptance[0])
	}
}

func TestUpdate_ConflictingAppendAndReplace(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "s"})
	_, err := Update(context.Background(), db, pid, q.ID, UpdateParams{
		Files:        []string{"a.go"},
		ReplaceFiles: []string{"b.go"},
	})
	if !errors.Is(err, ErrConflictingUpdate) {
		t.Errorf("err = %v, want ErrConflictingUpdate", err)
	}
}

func TestUpdate_NoChange(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "s"})
	_, err := Update(context.Background(), db, pid, q.ID, UpdateParams{})
	if !errors.Is(err, ErrNoChange) {
		t.Errorf("err = %v, want ErrNoChange", err)
	}
}

func TestUpdate_AutoBlockOnNewDep(t *testing.T) {
	db, pid := newTestDB(t)
	a := mustPost(t, db, pid, PostParams{Subject: "A"}) // next
	b := mustPost(t, db, pid, PostParams{Subject: "B"}) // next, no deps

	// Add a dep → B must flip to blocked.
	if _, err := Update(context.Background(), db, pid, b.ID, UpdateParams{
		DependsOn: []string{a.ID},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if s := mustStatus(t, db, pid, b.ID); s != StatusBlocked {
		t.Errorf("B status = %s, want blocked after adding dep on A (not done)", s)
	}
	// Now clear A — B must unblock.
	res, err := Clear(context.Background(), db, pid, a.ID, "")
	if err != nil {
		t.Fatalf("Clear A: %v", err)
	}
	if len(res.Unblocked) != 1 || res.Unblocked[0].ID != b.ID {
		t.Errorf("unblocked = %v, want [%s]", idsOf(res.Unblocked), b.ID)
	}
}

func TestUpdate_ClearAcceptance(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{
		Subject:    "s",
		Acceptance: []string{"a", "b"},
	})
	if _, err := Update(context.Background(), db, pid, q.ID, UpdateParams{
		ClearAcceptance: true,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := mustLoad(t, db, pid, q.ID)
	if len(got.Acceptance) != 0 {
		t.Errorf("acceptance = %v, want empty", got.Acceptance)
	}
}
