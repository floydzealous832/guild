package lore

import (
	"context"
	"testing"
	"time"
)

func TestOath_ProjectRequired(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := Oath(ctx, db, "")
	if err == nil {
		t.Fatalf("Oath without project should fail")
	}
}

func TestOath_ReturnsOnlyCurrentPrinciples(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES ('p','/tmp/p')`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()

	// Mix: principle/current (included), principle/archived (excluded),
	// decision/current (excluded).
	for _, row := range []struct {
		kind, status, title string
	}{
		{"principle", "current", "principle A"},
		{"principle", "current", "principle B"},
		{"principle", "archived", "archived principle"},
		{"decision", "current", "a decision"},
	} {
		_, err := db.ExecContext(ctx,
			`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
			 VALUES ('p','t',?,?,'summary',?,?,?)`,
			row.kind, row.title, row.status, now.Format(time.RFC3339), now.Format(time.RFC3339))
		if err != nil {
			t.Fatal(err)
		}
	}

	got, err := Oath(ctx, db, "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Oath returned %d, want 2", len(got))
	}
	for _, e := range got {
		if e.Kind != KindPrinciple {
			t.Errorf("non-principle in oath: %v", e.Kind)
		}
		if e.Status != StatusCurrent {
			t.Errorf("non-current in oath: %v", e.Status)
		}
	}
}

func TestOath_ScopedToProject(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	for _, pid := range []string{"p1", "p2"} {
		_, _ = db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES (?, ?)`, pid, "/tmp/"+pid)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, pid := range []string{"p1", "p1", "p2"} {
		_, err := db.ExecContext(ctx,
			`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
			 VALUES (?,?,?,?,'summary','current',?,?)`,
			pid, "t", "principle", "oath-"+pid, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}

	got1, err := Oath(ctx, db, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got1) != 2 {
		t.Fatalf("p1 oath: got %d, want 2", len(got1))
	}
	got2, err := Oath(ctx, db, "p2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got2) != 1 {
		t.Fatalf("p2 oath: got %d, want 1", len(got2))
	}
}
