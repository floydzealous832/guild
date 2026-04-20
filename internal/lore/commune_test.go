package lore

import (
	"context"
	"testing"
)

func TestCommune_BasicShape(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "proj-commune")

	// Insert a handful of entries.
	insertEntry := func(kind, title, summary, status string) {
		t.Helper()
		_, err := db.ExecContext(ctx,
			`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
			 VALUES ('proj-commune', 'test', ?, ?, ?, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			kind, title, summary, status,
		)
		if err != nil {
			t.Fatalf("insert %s entry: %v", kind, err)
		}
	}

	// Short principle (no bloat).
	insertEntry("principle", "Keep principles concise and actionable", "", "current")
	// Medium principle.
	insertEntry("principle", buildWords(40), "summary here", "current")
	// Bloat principle (>60 words).
	insertEntry("principle", buildWords(50), buildWords(20), "current")
	// Some non-principle entries.
	insertEntry("research", "Some research finding", "Detailed research.", "current")
	insertEntry("decision", "Architecture decision", "We chose X because Y.", "current")

	report, err := Commune(ctx, db, "proj-commune", false, false, 60, 120)
	if err != nil {
		t.Fatalf("Commune: %v", err)
	}

	if report == nil {
		t.Fatal("Commune returned nil report")
	}

	// Should have 3 principles total.
	if report.TotalPrinciples != 3 {
		t.Errorf("TotalPrinciples = %d; want 3", report.TotalPrinciples)
	}
	// Should detect 1 bloat principle.
	if report.BloatCount != 1 {
		t.Errorf("BloatCount = %d; want 1", report.BloatCount)
	}
	// SevereCount should be 0 (bloat principle is 70 words, < 120).
	if report.SevereCount != 0 {
		t.Errorf("SevereCount = %d; want 0 (word count < 120)", report.SevereCount)
	}
	// No dups in this test.
	if report.DupPairCount != 0 {
		t.Errorf("DupPairCount = %d; want 0", report.DupPairCount)
	}
}

func TestCommune_Fix_DemotesBloat(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "proj-fix")

	// Insert a principle with >120 words — severe bloat, fix=true should demote it.
	_, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('proj-fix', 'test', 'principle', ?, ?, 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		buildWords(80), buildWords(60), // 140 words total — severe
	)
	if err != nil {
		t.Fatalf("insert severe bloat principle: %v", err)
	}
	// Get the ID.
	var id int64
	err = db.QueryRowContext(ctx, `SELECT id FROM entries WHERE project_id = 'proj-fix'`).Scan(&id)
	if err != nil {
		t.Fatalf("get entry id: %v", err)
	}

	report, err := Commune(ctx, db, "proj-fix", false, true, 60, 120)
	if err != nil {
		t.Fatalf("Commune --fix: %v", err)
	}

	// Should have applied a reclassify fix.
	if len(report.FixesApplied) != 1 {
		t.Fatalf("len(FixesApplied) = %d; want 1", len(report.FixesApplied))
	}
	if report.FixesApplied[0].Kind != "reclassify" {
		t.Errorf("FixesApplied[0].Kind = %q; want reclassify", report.FixesApplied[0].Kind)
	}
	if report.FixesApplied[0].EntryID != id {
		t.Errorf("FixesApplied[0].EntryID = %d; want %d", report.FixesApplied[0].EntryID, id)
	}

	// Verify the DB was actually updated.
	var kindAfter string
	err = db.QueryRowContext(ctx, `SELECT kind FROM entries WHERE id = ?`, id).Scan(&kindAfter)
	if err != nil {
		t.Fatalf("re-read kind: %v", err)
	}
	if kindAfter != "decision" {
		t.Errorf("kind after fix = %q; want decision", kindAfter)
	}
}

func TestCommune_Fix_LighterBloatNotDemoted(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "proj-light")

	// Insert a principle with 70 words — bloat but not severe (< 120).
	_, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('proj-light', 'test', 'principle', ?, ?, 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		buildWords(40), buildWords(30), // 70 words — bloat but < 120
	)
	if err != nil {
		t.Fatalf("insert light bloat principle: %v", err)
	}

	report, err := Commune(ctx, db, "proj-light", false, true, 60, 120)
	if err != nil {
		t.Fatalf("Commune --fix: %v", err)
	}

	// Light bloat should be surfaced (BloatCount=1) but NOT auto-fixed.
	if report.BloatCount != 1 {
		t.Errorf("BloatCount = %d; want 1", report.BloatCount)
	}
	if report.SevereCount != 0 {
		t.Errorf("SevereCount = %d; want 0 (< severeBoundary)", report.SevereCount)
	}
	// No reclassify fix should have been applied.
	for _, fix := range report.FixesApplied {
		if fix.Kind == "reclassify" {
			t.Errorf("unexpected reclassify fix for light bloat: %+v", fix)
		}
	}

	// Verify kind is still "principle".
	var kindAfter string
	err = db.QueryRowContext(ctx, `SELECT kind FROM entries WHERE project_id = 'proj-light'`).Scan(&kindAfter)
	if err != nil {
		t.Fatalf("re-read kind: %v", err)
	}
	if kindAfter != "principle" {
		t.Errorf("kind after fix = %q; want principle (light bloat should not be demoted)", kindAfter)
	}
}

func TestCommune_Fix_DupPairsReforged(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "proj-dup-fix-a", "proj-dup-fix-b")

	sharedTitle := "Duplicate entry across projects should be reforged"
	sharedSummary := "Both projects contain identical text."

	_, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('proj-dup-fix-a', 'test', 'principle', ?, ?, 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		sharedTitle, sharedSummary,
	)
	if err != nil {
		t.Fatalf("insert proj-dup-fix-a: %v", err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('proj-dup-fix-b', 'test', 'principle', ?, ?, 'current', '2026-01-02T00:00:00Z', '2026-01-02T00:00:00Z')`,
		sharedTitle, sharedSummary,
	)
	if err != nil {
		t.Fatalf("insert proj-dup-fix-b: %v", err)
	}

	report, err := Commune(ctx, db, "", true, true, 60, 120)
	if err != nil {
		t.Fatalf("Commune --fix: %v", err)
	}

	// Should have found and reforged the dup pair.
	if report.DupPairCount != 1 {
		t.Errorf("DupPairCount = %d; want 1", report.DupPairCount)
	}

	// Verify at least one reforge fix was applied.
	reforgeCount := 0
	for _, fix := range report.FixesApplied {
		if fix.Kind == "reforge" {
			reforgeCount++
		}
	}
	if reforgeCount != 1 {
		t.Errorf("reforge fixes = %d; want 1", reforgeCount)
	}

	// Verify the older entry (lower id) is now superseded.
	var statusOlder string
	err = db.QueryRowContext(ctx,
		`SELECT status FROM entries WHERE id = (SELECT MIN(id) FROM entries)`,
	).Scan(&statusOlder)
	if err != nil {
		t.Fatalf("re-read older entry status: %v", err)
	}
	if statusOlder != "superseded" {
		t.Errorf("older entry status = %q; want superseded", statusOlder)
	}
}

func TestCommune_AllProjects(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "proj-all-a", "proj-all-b")

	// Insert one principle in each project.
	_, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('proj-all-a', 'test', 'principle', 'A principle', 'Short.', 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert proj-all-a: %v", err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('proj-all-b', 'test', 'principle', 'Another principle', 'Also short.', 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert proj-all-b: %v", err)
	}

	report, err := Commune(ctx, db, "", true, false, 60, 120)
	if err != nil {
		t.Fatalf("Commune --all-projects: %v", err)
	}
	if report.TotalPrinciples != 2 {
		t.Errorf("TotalPrinciples = %d; want 2", report.TotalPrinciples)
	}
}
