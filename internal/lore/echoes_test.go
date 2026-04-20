package lore

import (
	"context"
	"testing"
	"time"
)

func TestEchoes_ProjectRequired(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := Echoes(ctx, db, "", false)
	if err == nil {
		t.Fatalf("Echoes without project should fail")
	}
}

func TestEchoes_ValidDaysElapsed(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES ('p','/tmp/p')`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old := now.AddDate(0, 0, -40).Format(time.RFC3339)
	fresh := now.AddDate(0, 0, -5).Format(time.RFC3339)

	// Old entry with 30-day valid_days → stale
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, valid_days, created_at, updated_at)
		 VALUES ('p','t','research','old one','summary','current',30,?,?)`, old, old)
	if err != nil {
		t.Fatal(err)
	}
	// Fresh entry with 30-day valid_days → still good
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, valid_days, created_at, updated_at)
		 VALUES ('p','t','research','fresh one','summary','current',30,?,?)`, fresh, fresh)
	if err != nil {
		t.Fatal(err)
	}
	// Never-stales entry (valid_days NULL) → still good
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('p','t','principle','eternal','summary','current',?,?)`, old, old)
	if err != nil {
		t.Fatal(err)
	}

	stale, err := Echoes(ctx, db, "p", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 {
		t.Fatalf("got %d stale, want 1; results=%+v", len(stale), stale)
	}
	if stale[0].Entry.Title != "old one" {
		t.Fatalf("wrong entry flagged: %q", stale[0].Entry.Title)
	}
}

func TestEchoes_SkipsArchived(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES ('p','/tmp/p')`)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().AddDate(0, 0, -40).Format(time.RFC3339)
	// Archived entry — even though old, should not be returned
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, valid_days, created_at, updated_at)
		 VALUES ('p','t','research','archived old','summary','archived',30,?,?)`, old, old)
	if err != nil {
		t.Fatal(err)
	}

	stale, err := Echoes(ctx, db, "p", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("archived entries should not appear; got %d", len(stale))
	}
}

func TestEchoes_GitAwareSkippedWhenFilePathEmpty(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES ('p','/tmp/p')`)
	if err != nil {
		t.Fatal(err)
	}
	// Current, valid_days=NULL, no file_path → git-aware should NOT flag it.
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('p','t','research','no file','summary','current',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}

	stale, err := Echoes(ctx, db, "p", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("expected 0 stale; got %d", len(stale))
	}
}
