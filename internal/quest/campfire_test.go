package quest

import (
	"context"
	"strings"
	"testing"
)

func TestCampfire_RoundTrip(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	q := mustPost(t, db, pid, PostParams{Subject: "deep refactor"})

	p := CampfireParams{
		Hypothesis:   "the issue is in the parser",
		Tried:        []string{"approach A", "approach B"},
		Next:         "try the lexer instead",
		TokenWarning: false,
		Agent:        "alice",
	}
	if err := Campfire(ctx, db, pid, q.ID, p); err != nil {
		t.Fatalf("Campfire: %v", err)
	}

	// Scroll should surface the campfire note with [checkpoint] prefix.
	res, err := Scroll(ctx, db, pid, q.ID)
	if err != nil {
		t.Fatalf("Scroll: %v", err)
	}

	var campfireNote string
	for _, n := range res.Notes {
		if strings.HasPrefix(n.Note, NotePrefixCheckpoint) {
			// Skip the accept-trail checkpoint; we want the campfire one.
			if strings.Contains(n.Note, "hypothesis:") {
				campfireNote = n.Note
			}
		}
	}

	if campfireNote == "" {
		t.Fatal("campfire [checkpoint] note not found in scroll")
	}
	if !strings.Contains(campfireNote, "hypothesis: the issue is in the parser") {
		t.Errorf("hypothesis not found in note: %q", campfireNote)
	}
	if !strings.Contains(campfireNote, "tried: approach A; approach B") {
		t.Errorf("tried not found in note: %q", campfireNote)
	}
	if !strings.Contains(campfireNote, "next: try the lexer instead") {
		t.Errorf("next not found in note: %q", campfireNote)
	}

	// Verify checkpoint event emitted.
	foundEvent := false
	for _, e := range res.Events {
		if e.Event == "checkpoint" {
			foundEvent = true
		}
	}
	if !foundEvent {
		t.Error("checkpoint event not found in scroll timeline")
	}
}

func TestCampfire_TokenWarning(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	q := mustPost(t, db, pid, PostParams{Subject: "long task"})

	p := CampfireParams{
		TokenWarning: true,
		Next:         "resume here",
	}
	if err := Campfire(ctx, db, pid, q.ID, p); err != nil {
		t.Fatalf("Campfire: %v", err)
	}

	res, err := Scroll(ctx, db, pid, q.ID)
	if err != nil {
		t.Fatalf("Scroll: %v", err)
	}

	var found bool
	for _, n := range res.Notes {
		if strings.Contains(n.Note, "token_warning: true") {
			found = true
		}
	}
	if !found {
		t.Error("token_warning not found in campfire note")
	}
}

func TestCampfire_Empty_Error(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	q := mustPost(t, db, pid, PostParams{Subject: "something"})

	err := Campfire(ctx, db, pid, q.ID, CampfireParams{})
	if err == nil {
		t.Fatal("expected error when no campfire fields set")
	}
}

func TestCampfire_NotFound(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	err := Campfire(ctx, db, pid, "QUEST-999", CampfireParams{Hypothesis: "test"})
	if err == nil {
		t.Fatal("expected error for missing quest")
	}
}

func TestBuildCampfireSnapshot(t *testing.T) {
	p := CampfireParams{
		Hypothesis:   "H",
		Tried:        []string{"A", "B"},
		Next:         "N",
		TokenWarning: true,
	}
	snap := buildCampfireSnapshot(p)
	want := "hypothesis: H | tried: A; B | next: N | token_warning: true"
	if snap != want {
		t.Errorf("buildCampfireSnapshot\ngot:  %q\nwant: %q", snap, want)
	}
}

func TestBuildCampfireSnapshot_Partial(t *testing.T) {
	p := CampfireParams{Hypothesis: "only hypothesis"}
	snap := buildCampfireSnapshot(p)
	if snap != "hypothesis: only hypothesis" {
		t.Errorf("unexpected snapshot: %q", snap)
	}
}
