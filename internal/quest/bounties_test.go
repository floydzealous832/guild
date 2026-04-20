package quest

import (
	"context"
	"errors"
	"testing"
)

// nilLoaders is a convenience for tests that don't exercise lore.
var nilLoaders = struct {
	oath OathLoader
	echo EchoLoader
}{nil, nil}

func TestBounties_EmptyProject(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	res, err := Bounties(ctx, db, pid, false, nilLoaders.oath, nilLoaders.echo)
	if err != nil {
		t.Fatalf("Bounties: %v", err)
	}
	if !res.NoUnclaimed {
		t.Error("expected NoUnclaimed=true when project has no quests")
	}
	if res.TopQuest != nil {
		t.Error("expected nil TopQuest for empty project")
	}
}

func TestBounties_TopTask_HighestPriority(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	// Post quests at mixed priorities — expect P0 to surface as top.
	q2 := mustPost(t, db, pid, PostParams{Subject: "medium work", Priority: "P2"})
	q0 := mustPost(t, db, pid, PostParams{Subject: "critical fix", Priority: "P0"})
	_ = mustPost(t, db, pid, PostParams{Subject: "nice to have", Priority: "P3"})

	_ = q2

	res, err := Bounties(ctx, db, pid, false, nilLoaders.oath, nilLoaders.echo)
	if err != nil {
		t.Fatalf("Bounties: %v", err)
	}
	if res.TopQuest == nil {
		t.Fatal("expected non-nil TopQuest")
	}
	if res.TopQuest.ID != q0.ID {
		t.Errorf("TopQuest = %q, want P0 quest %q", res.TopQuest.ID, q0.ID)
	}
	if res.NoUnclaimed {
		t.Error("NoUnclaimed should be false")
	}
}

func TestBounties_ParallelismDetection(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	// Three next quests at same priority:
	//   qA: files=[a.go, b.go]  — the top quest (first by ID)
	//   qB: files=[a.go]        — shares a.go with qA → NOT parallel
	//   qC: files=[c.go]        — disjoint → IS parallel

	qA := mustPost(t, db, pid, PostParams{
		Subject:  "quest A",
		Priority: "P1",
		Files:    []string{"a.go", "b.go"},
	})
	qB := mustPost(t, db, pid, PostParams{
		Subject:  "quest B",
		Priority: "P1",
		Files:    []string{"a.go"},
	})
	qC := mustPost(t, db, pid, PostParams{
		Subject:  "quest C",
		Priority: "P1",
		Files:    []string{"c.go"},
	})

	res, err := Bounties(ctx, db, pid, false, nilLoaders.oath, nilLoaders.echo)
	if err != nil {
		t.Fatalf("Bounties: %v", err)
	}

	if res.TopQuest == nil {
		t.Fatal("expected non-nil TopQuest")
	}
	// qA should be top (lowest ID among equal-priority quests).
	if res.TopQuest.ID != qA.ID {
		t.Errorf("TopQuest = %q, want %q", res.TopQuest.ID, qA.ID)
	}

	// ParallelCount should be 1: only qC is disjoint.
	if res.ParallelCount != 1 {
		t.Errorf("ParallelCount = %d, want 1; qB has shared file, qC does not", res.ParallelCount)
	}

	// The one parallel pair should be (qA, qC).
	if len(res.ParallelPairs) != 1 {
		t.Fatalf("ParallelPairs len = %d, want 1", len(res.ParallelPairs))
	}
	if res.ParallelPairs[0].A != qA.ID || res.ParallelPairs[0].B != qC.ID {
		t.Errorf("ParallelPairs[0] = {%s, %s}, want {%s, %s}",
			res.ParallelPairs[0].A, res.ParallelPairs[0].B, qA.ID, qC.ID)
	}

	_ = qB
}

func TestBounties_BriefMode(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	// Write a brief.
	if err := Brief(ctx, db, pid, "wrap up: tests passing", "carol"); err != nil {
		t.Fatalf("Brief: %v", err)
	}

	// Post some quests.
	mustPost(t, db, pid, PostParams{Subject: "task 1", Priority: "P0"})

	// Call in brief-only mode.
	res, err := Bounties(ctx, db, pid, true, nilLoaders.oath, nilLoaders.echo)
	if err != nil {
		t.Fatalf("Bounties(briefOnly=true): %v", err)
	}

	// Brief fields should be populated.
	if res.LastBriefText != "wrap up: tests passing" {
		t.Errorf("LastBriefText = %q", res.LastBriefText)
	}
	if res.LastBriefAgent != "carol" {
		t.Errorf("LastBriefAgent = %q", res.LastBriefAgent)
	}
	if res.LastBriefAt == "" {
		t.Error("LastBriefAt should be set")
	}

	// Non-brief fields should be zero/empty (brief-only mode).
	if res.TopQuest != nil {
		t.Error("TopQuest should be nil in brief-only mode")
	}
	if len(res.AllNext) != 0 {
		t.Error("AllNext should be empty in brief-only mode")
	}
	if len(res.Oath) != 0 {
		t.Error("Oath should be empty in brief-only mode")
	}
}

func TestBounties_OathLoader_Called(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	called := false
	oathLoader := func(ctx context.Context, project string) ([]OathEntry, error) {
		called = true
		if project != pid {
			t.Errorf("oathLoader got project %q, want %q", project, pid)
		}
		return []OathEntry{{Title: "the principle", Summary: "do good work"}}, nil
	}

	_, err := Bounties(ctx, db, pid, false, oathLoader, nil)
	if err != nil {
		t.Fatalf("Bounties: %v", err)
	}
	if !called {
		t.Error("oathLoader was not called")
	}
}

func TestBounties_OathLoader_ErrorDegradesGracefully(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	oathLoader := func(ctx context.Context, project string) ([]OathEntry, error) {
		return nil, errors.New("lore DB unavailable")
	}

	// Should not fail even if oath loader returns an error.
	res, err := Bounties(ctx, db, pid, false, oathLoader, nil)
	if err != nil {
		t.Fatalf("Bounties should not fail on oath loader error; got %v", err)
	}
	if len(res.Oath) != 0 {
		t.Error("Oath should be empty when loader errors")
	}
}

func TestBounties_ClaimedQuestsExcluded(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	q := mustPost(t, db, pid, PostParams{Subject: "already taken"})
	// Accept the quest so it's in_progress.
	if _, err := Accept(ctx, db, pid, q.ID, "someagent"); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	res, err := Bounties(ctx, db, pid, false, nil, nil)
	if err != nil {
		t.Fatalf("Bounties: %v", err)
	}
	// Only in_progress quest — nothing available.
	if !res.NoUnclaimed {
		t.Error("expected NoUnclaimed when only quest is in_progress")
	}
}

func TestBounties_NoBrief_Fields_Empty(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	res, err := Bounties(ctx, db, pid, false, nil, nil)
	if err != nil {
		t.Fatalf("Bounties: %v", err)
	}
	if res.LastBriefText != "" || res.LastBriefAgent != "" || res.LastBriefAt != "" {
		t.Error("expected empty brief fields when no brief written")
	}
}

func TestBounties_ParallelPairsMax5(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	// Post 7 quests: one top (files=[shared.go]) + 6 disjoint.
	mustPost(t, db, pid, PostParams{Subject: "top", Priority: "P0", Files: []string{"shared.go"}})
	for i := 0; i < 6; i++ {
		mustPost(t, db, pid, PostParams{
			Subject:  "parallel task",
			Priority: "P1",
			Files:    []string{},
		})
	}

	res, err := Bounties(ctx, db, pid, false, nil, nil)
	if err != nil {
		t.Fatalf("Bounties: %v", err)
	}
	if res.ParallelCount != 6 {
		t.Errorf("ParallelCount = %d, want 6", res.ParallelCount)
	}
	// Pairs shown capped at 5.
	if len(res.ParallelPairs) != 5 {
		t.Errorf("ParallelPairs displayed = %d, want 5", len(res.ParallelPairs))
	}
}
