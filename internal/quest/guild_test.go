package quest

import (
	"context"
	"testing"
)

func TestGuild_Empty(t *testing.T) {
	db, pid := newTestDB(t)

	summary, err := Guild(context.Background(), db, pid)
	if err != nil {
		t.Fatalf("Guild: %v", err)
	}
	if summary.ProjectID != pid {
		t.Errorf("ProjectID = %q, want %q", summary.ProjectID, pid)
	}
	if len(summary.Epics) != 0 {
		t.Errorf("want 0 epics, got %d", len(summary.Epics))
	}
	if summary.Totals.Next != 0 {
		t.Errorf("totals.Next = %d, want 0", summary.Totals.Next)
	}
}

func TestGuild_MultiEpic(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	// 3 quests across 2 epics.
	q1 := mustPost(t, db, pid, PostParams{Subject: "alpha-1", Epic: "alpha"})
	mustPost(t, db, pid, PostParams{Subject: "alpha-2", Epic: "alpha"})
	mustPost(t, db, pid, PostParams{Subject: "beta-1", Epic: "beta"})

	// Accept one alpha quest.
	if _, err := Accept(ctx, db, pid, q1.ID, "agent"); err != nil {
		t.Fatalf("accept: %v", err)
	}

	summary, err := Guild(ctx, db, pid)
	if err != nil {
		t.Fatalf("Guild: %v", err)
	}

	if len(summary.Epics) != 2 {
		t.Fatalf("want 2 epics, got %d", len(summary.Epics))
	}

	// Find alpha and beta.
	var alpha, beta *EpicSummary
	for i := range summary.Epics {
		switch summary.Epics[i].Epic {
		case "alpha":
			alpha = &summary.Epics[i]
		case "beta":
			beta = &summary.Epics[i]
		}
	}
	if alpha == nil || beta == nil {
		t.Fatal("didn't find both epics")
	}

	// alpha: 1 next, 1 in_progress, 0 done.
	if alpha.Next != 1 {
		t.Errorf("alpha.Next = %d, want 1", alpha.Next)
	}
	if alpha.InProgress != 1 {
		t.Errorf("alpha.InProgress = %d, want 1", alpha.InProgress)
	}
	if alpha.Done != 0 {
		t.Errorf("alpha.Done = %d, want 0", alpha.Done)
	}

	// beta: 1 next.
	if beta.Next != 1 {
		t.Errorf("beta.Next = %d, want 1", beta.Next)
	}

	// Totals.
	if summary.Totals.Next != 2 {
		t.Errorf("totals.Next = %d, want 2", summary.Totals.Next)
	}
	if summary.Totals.InProgress != 1 {
		t.Errorf("totals.InProgress = %d, want 1", summary.Totals.InProgress)
	}
}

func TestGuild_NoEpic(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	// Quest with no epic falls into "(none)".
	mustPost(t, db, pid, PostParams{Subject: "no epic quest"})

	summary, err := Guild(ctx, db, pid)
	if err != nil {
		t.Fatalf("Guild: %v", err)
	}
	if len(summary.Epics) != 1 {
		t.Fatalf("want 1 epic group, got %d", len(summary.Epics))
	}
	if summary.Epics[0].Epic != "(none)" {
		t.Errorf("epic = %q, want (none)", summary.Epics[0].Epic)
	}
}

func TestGuild_CountsDone(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	q1 := mustPost(t, db, pid, PostParams{Subject: "task1", Epic: "e"})
	mustPost(t, db, pid, PostParams{Subject: "task2", Epic: "e"})

	// Accept and clear q1.
	if _, err := Accept(ctx, db, pid, q1.ID, "agent"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, err := Clear(ctx, db, pid, q1.ID, "done"); err != nil {
		t.Fatalf("clear: %v", err)
	}

	summary, err := Guild(ctx, db, pid)
	if err != nil {
		t.Fatalf("Guild: %v", err)
	}
	if len(summary.Epics) != 1 {
		t.Fatalf("want 1 epic group, got %d", len(summary.Epics))
	}
	e := summary.Epics[0]
	if e.Done != 1 {
		t.Errorf("Done = %d, want 1", e.Done)
	}
	if e.Next != 1 {
		t.Errorf("Next = %d, want 1", e.Next)
	}
}
