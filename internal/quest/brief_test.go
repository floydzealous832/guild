package quest

import (
	"context"
	"testing"
)

func TestBrief_RoundTrip(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	text := "session ended well; next up is the auth refactor"
	if err := Brief(ctx, db, pid, text, "alice"); err != nil {
		t.Fatalf("Brief: %v", err)
	}

	agent, got, at, err := LastBrief(ctx, db, pid)
	if err != nil {
		t.Fatalf("LastBrief: %v", err)
	}
	if got != text {
		t.Errorf("LastBrief text\ngot:  %q\nwant: %q", got, text)
	}
	if agent != "alice" {
		t.Errorf("LastBrief agent = %q, want %q", agent, "alice")
	}
	if at.IsZero() {
		t.Error("LastBrief returned zero time")
	}
}

func TestBrief_LastWins(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	if err := Brief(ctx, db, pid, "first brief", "agent"); err != nil {
		t.Fatalf("Brief 1: %v", err)
	}
	if err := Brief(ctx, db, pid, "second brief", "agent"); err != nil {
		t.Fatalf("Brief 2: %v", err)
	}

	_, got, _, err := LastBrief(ctx, db, pid)
	if err != nil {
		t.Fatalf("LastBrief: %v", err)
	}
	if got != "second brief" {
		t.Errorf("expected last brief; got %q", got)
	}
}

func TestBrief_EmptyText_Error(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	if err := Brief(ctx, db, pid, "", "agent"); err == nil {
		t.Fatal("expected error for empty brief text")
	}
}

func TestLastBrief_NoBrief(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	agent, text, at, err := LastBrief(ctx, db, pid)
	if err != nil {
		t.Fatalf("LastBrief: %v", err)
	}
	if agent != "" || text != "" || !at.IsZero() {
		t.Errorf("expected empty result for project with no brief; got agent=%q text=%q", agent, text)
	}
}
