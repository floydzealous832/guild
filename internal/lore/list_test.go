package lore

import (
	"context"
	"testing"
	"time"
)

func TestList_ProjectRequired(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := List(ctx, db, ListFilters{})
	if err == nil {
		t.Fatalf("List without project should fail")
	}
}

func TestList_BasicFilters(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	for _, pid := range []string{"p1", "p2"} {
		_, _ = db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES (?, ?)`, pid, "/tmp/"+pid)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	insertList := func(pid, topic, kind, title string) {
		_, err := db.ExecContext(ctx,
			`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 'x','current',?,?)`,
			pid, topic, kind, title, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	insertList("p1", "auth", "research", "research-1")
	insertList("p1", "auth", "decision", "decision-1")
	insertList("p1", "ui", "research", "research-2")
	insertList("p2", "auth", "research", "research-3")

	// Project scope filters out p2
	entries, err := List(ctx, db, ListFilters{Project: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("project=p1: got %d, want 3", len(entries))
	}

	// Topic filter
	entries, err = List(ctx, db, ListFilters{Project: "p1", Topic: "auth"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("topic=auth: got %d, want 2", len(entries))
	}

	// Kind filter
	entries, err = List(ctx, db, ListFilters{Project: "p1", Kind: KindResearch})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("kind=research: got %d, want 2", len(entries))
	}
}

func TestList_DefaultHidesArchivedAndSuperseded(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	seedCorpus(t, ctx, db, []fixtureEntry{
		{"p", "t", "live entry", "x", "t"},
		{"p", "t", "archived entry", "x", "t"},
		{"p", "t", "superseded entry", "x", "t"},
	})
	_, _ = db.ExecContext(ctx, `UPDATE entries SET status = 'archived' WHERE title = 'archived entry'`)
	_, _ = db.ExecContext(ctx, `UPDATE entries SET status = 'superseded' WHERE title = 'superseded entry'`)

	entries, err := List(ctx, db, ListFilters{Project: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("default status filter: got %d, want 1", len(entries))
	}

	// Explicit status=archived returns the archived entry
	entries, err = List(ctx, db, ListFilters{Project: "p", Status: StatusArchived})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("status=archived: got %d, want 1", len(entries))
	}
	if entries[0].Status != StatusArchived {
		t.Fatalf("wrong status: %q", entries[0].Status)
	}
}

func TestList_FilePathFilter(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES ('p','/tmp/p')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, file_path, status, created_at, updated_at)
		 VALUES ('p','t','research','a','summary mentioning foo','foo/bar.md','current',datetime('now'),datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('p','t','research','b','unrelated','current',datetime('now'),datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}

	entries, err := List(ctx, db, ListFilters{Project: "p", FilePath: "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("file filter: got %d, want 1", len(entries))
	}
	if entries[0].Title != "a" {
		t.Fatalf("wrong entry returned: %q", entries[0].Title)
	}
}
