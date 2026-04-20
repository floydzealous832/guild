package hints

import (
	"context"
	"testing"
	"time"
)

// TestPrune_BelowFloorDemotes sends enough synthetic misses through a
// hint rule to trip the prune floor, then asserts the rule demotes to fyi.
func TestPrune_BelowFloorDemotes(t *testing.T) {
	store, _ := newTestStore(t)
	eng := NewEngine(store, "s1", EraMCP)
	if err := eng.LoadRules(context.Background()); err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	ctx := context.Background()

	// Write 30 scored miss fires against inscribe-looks-like-quest (a hint).
	for i := 0; i < 30; i++ {
		id, err := store.RecordFire(ctx, "inscribe-looks-like-quest", "", "s1", time.Now())
		if err != nil {
			t.Fatalf("record: %v", err)
		}
		if err := store.RecordFollowThrough(ctx, id, false, FollowThroughWindow); err != nil {
			t.Fatalf("score: %v", err)
		}
	}

	actions, err := eng.Prune(ctx)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	found := false
	for _, a := range actions {
		if a.RuleID == "inscribe-looks-like-quest" {
			found = true
			if a.Before != SeverityHint || a.After != SeverityFYI {
				t.Errorf("demote shape: %+v", a)
			}
		}
	}
	if !found {
		t.Errorf("expected a prune action on inscribe-looks-like-quest; actions=%+v", actions)
	}

	// Verify the DB row changed.
	rr, err := store.GetRule(ctx, "inscribe-looks-like-quest")
	if err != nil {
		t.Fatalf("GetRule: %v", err)
	}
	if rr.Severity != SeverityFYI {
		t.Errorf("severity after prune = %s, want fyi", rr.Severity)
	}
}

// TestPrune_FYIBelowFloorDisables drops a fyi-tier rule below floor and
// verifies prune sets enabled=0.
func TestPrune_FYIBelowFloorDisables(t *testing.T) {
	store, _ := newTestStore(t)
	eng := NewEngine(store, "s1", EraMCP)
	if err := eng.LoadRules(context.Background()); err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	ctx := context.Background()

	for i := 0; i < 30; i++ {
		id, err := store.RecordFire(ctx, "clear-without-report-detail", "", "s1", time.Now())
		if err != nil {
			t.Fatalf("record: %v", err)
		}
		if err := store.RecordFollowThrough(ctx, id, false, FollowThroughWindow); err != nil {
			t.Fatalf("score: %v", err)
		}
	}

	actions, err := eng.Prune(ctx)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	found := false
	for _, a := range actions {
		if a.RuleID == "clear-without-report-detail" && a.Disabled {
			found = true
		}
	}
	if !found {
		t.Errorf("expected disable action on clear-without-report-detail; got %+v", actions)
	}
	rr, err := store.GetRule(ctx, "clear-without-report-detail")
	if err != nil {
		t.Fatalf("GetRule: %v", err)
	}
	if rr.Enabled {
		t.Error("clear-without-report-detail should be disabled after prune")
	}
}

// TestPrune_AboveFloorStays asserts rules above the floor are untouched.
func TestPrune_AboveFloorStays(t *testing.T) {
	store, _ := newTestStore(t)
	eng := NewEngine(store, "s1", EraMCP)
	if err := eng.LoadRules(context.Background()); err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	ctx := context.Background()

	// 20 fires, 8 hits → 40% hit rate, well above floor.
	for i := 0; i < 20; i++ {
		id, err := store.RecordFire(ctx, "slug-query", "", "s1", time.Now())
		if err != nil {
			t.Fatalf("record: %v", err)
		}
		hit := i < 8
		if err := store.RecordFollowThrough(ctx, id, hit, 3); err != nil {
			t.Fatalf("score: %v", err)
		}
	}

	actions, err := eng.Prune(ctx)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	for _, a := range actions {
		if a.RuleID == "slug-query" {
			t.Errorf("slug-query should not be pruned (40%% > 14.46%%); got %+v", a)
		}
	}
}

// TestPrune_BelowMinScoredSkips asserts fresh rules (< MinScoredBeforePrune)
// don't get demoted on a single bad streak.
func TestPrune_BelowMinScoredSkips(t *testing.T) {
	store, _ := newTestStore(t)
	eng := NewEngine(store, "s1", EraMCP)
	if err := eng.LoadRules(context.Background()); err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	ctx := context.Background()

	// Only 5 scored misses — below the statistical gate.
	for i := 0; i < 5; i++ {
		id, err := store.RecordFire(ctx, "journal-outside-accepted", "", "s1", time.Now())
		if err != nil {
			t.Fatalf("record: %v", err)
		}
		if err := store.RecordFollowThrough(ctx, id, false, 3); err != nil {
			t.Fatalf("score: %v", err)
		}
	}

	actions, err := eng.Prune(ctx)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	for _, a := range actions {
		if a.RuleID == "journal-outside-accepted" {
			t.Errorf("journal-outside-accepted should not be pruned below MinScored; got %+v", a)
		}
	}
}
