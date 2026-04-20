package lore

import (
	"context"
	"testing"
	"time"
)

func TestWhispers_ProjectRequired(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	if _, err := Whispers(ctx, db, "", ""); err == nil {
		t.Fatalf("Whispers without project should fail")
	}
}

func TestWhispers_FiltersIdeaSeedExploring(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES ('p','/tmp/p')`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	insert := func(kind, status, title, topic string) {
		_, err := db.ExecContext(ctx,
			`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
			 VALUES ('p',?,?,?,'summary',?,?,?)`,
			topic, kind, title, status, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("idea", "seed", "seed idea A", "auth")
	insert("idea", "exploring", "exploring idea", "ui")
	insert("idea", "promoted", "promoted idea", "auth")
	insert("idea", "parked", "parked idea", "auth")
	insert("decision", "current", "a decision", "auth")

	out, err := Whispers(ctx, db, "p", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d whispers, want 2 (seed + exploring)", len(out))
	}
	for _, e := range out {
		if e.Kind != KindIdea {
			t.Errorf("non-idea returned: %v", e.Kind)
		}
		if e.Status != StatusSeed && e.Status != StatusExploring {
			t.Errorf("wrong status: %v", e.Status)
		}
	}
}

func TestWhispers_TopicFilter(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES ('p','/tmp/p')`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('p','auth','idea','auth whisper','summary','seed',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('p','ui','idea','ui whisper','summary','seed',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}

	out, err := Whispers(ctx, db, "p", "auth")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("topic filter: got %d, want 1", len(out))
	}
	if out[0].Topic != "auth" {
		t.Fatalf("wrong topic: %q", out[0].Topic)
	}
}
