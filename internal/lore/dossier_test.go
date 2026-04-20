package lore

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDossier_ProjectRequired(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	if _, err := Dossier(ctx, db, ""); err == nil {
		t.Fatalf("Dossier without project should fail")
	}
}

func TestDossier_EmptyProjectReturnsHeaderOnly(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES ('p','/tmp/p')`)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Dossier(ctx, db, "p")
	if err != nil {
		t.Fatal(err)
	}
	if d.Project != "p" {
		t.Errorf("Project = %q, want p", d.Project)
	}
	if !strings.Contains(d.Text, "PROJECT DOSSIER: p") {
		t.Errorf("missing project header: %q", d.Text)
	}
}

func TestDossier_Sections(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES ('p','/tmp/p')`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	seed := func(kind, status, title, summary string) {
		_, err := db.ExecContext(ctx,
			`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
			 VALUES ('p','t',?,?,?,?,?,?)`,
			kind, title, summary, status, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	seed("principle", "current", "p1", "principle one summary")
	seed("principle", "current", "p2", "principle two summary")
	seed("decision", "current", "d1", "decision one summary")
	seed("decision", "current", "d2", "decision two summary")
	seed("observation", "current", "o1", "observation one summary")
	seed("idea", "seed", "i1", "idea one summary")

	d, err := Dossier(ctx, db, "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Principles) != 2 {
		t.Errorf("Principles=%d, want 2", len(d.Principles))
	}
	if len(d.Decisions) != 2 {
		t.Errorf("Decisions=%d, want 2", len(d.Decisions))
	}
	if len(d.Observations) != 1 {
		t.Errorf("Observations=%d, want 1", len(d.Observations))
	}
	if len(d.Whispers) != 1 {
		t.Errorf("Whispers=%d, want 1", len(d.Whispers))
	}
	for _, section := range []string{
		"PRINCIPLES", "KEY DECISIONS", "RECENT OBSERVATIONS", "CURRENT WHISPERS",
	} {
		if !strings.Contains(d.Text, section) {
			t.Errorf("dossier text missing section %q", section)
		}
	}
}

func TestDossier_TopAccessed(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES ('p','/tmp/p')`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < 6; i++ {
		_, err := db.ExecContext(ctx,
			`INSERT INTO entries (project_id, topic, kind, title, summary, status, access_count, last_accessed_at, created_at, updated_at)
			 VALUES ('p','t','research',?,'summary','current',?,?,?,?)`,
			fmt.Sprintf("entry %d", i), i, now, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	d, err := Dossier(ctx, db, "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.TopAccessed) != 5 {
		t.Fatalf("TopAccessed=%d, want 5 (cap)", len(d.TopAccessed))
	}
	// Should be sorted descending by access_count.
	if d.TopAccessed[0].Title != "entry 5" {
		t.Errorf("top entry = %q, want entry 5", d.TopAccessed[0].Title)
	}
}

// TestDossier_TokenBudget verifies the rendered text lands within the
// 1500-2500 approximate-token band. Uses the 4-chars-per-token heuristic
// approved by the quest acceptance.
func TestDossier_TokenBudget(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES ('p','/tmp/p')`)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	insert := func(kind, status, title, summary string) {
		_, err := db.ExecContext(ctx,
			`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
			 VALUES ('p','t',?,?,?,?,?,?)`,
			kind, title, summary, status, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Rich, realistic content — each summary gets truncated to 300
	// chars by compactSummary, so multiplied across ~28 bullets we
	// land in the 1500-2500 token band (6000-10000 bytes at 4-cpt).
	longSummary := strings.Repeat("one reason some context a caveat plus a followup detail ", 6) + "."
	for i := 0; i < 10; i++ {
		insert("principle", "current", fmt.Sprintf("principle %d with signal", i),
			fmt.Sprintf("principle %d summary: %s", i, longSummary))
	}
	for i := 0; i < 5; i++ {
		insert("decision", "current", fmt.Sprintf("decision %d with signal", i),
			fmt.Sprintf("decision %d summary: %s", i, longSummary))
	}
	for i := 0; i < 5; i++ {
		insert("observation", "current", fmt.Sprintf("observation %d with signal", i),
			fmt.Sprintf("observation %d summary: %s", i, longSummary))
	}
	for i := 0; i < 8; i++ {
		insert("idea", "seed", fmt.Sprintf("idea %d whisper", i),
			fmt.Sprintf("idea %d summary: %s", i, longSummary))
	}

	d, err := Dossier(ctx, db, "p")
	if err != nil {
		t.Fatal(err)
	}
	tokens := ApproxTokens(d.Text)
	t.Logf("dossier byte length = %d, approx tokens = %d", len(d.Text), tokens)
	if tokens < 1500 || tokens > 2500 {
		t.Errorf("dossier token estimate %d outside [1500,2500] band", tokens)
	}
}

func TestDossier_CompactSummary(t *testing.T) {
	in := "first line\nsecond  line    with   spaces"
	got := compactSummary(in)
	want := "first line second line with spaces"
	if got != want {
		t.Fatalf("compactSummary = %q, want %q", got, want)
	}
}
