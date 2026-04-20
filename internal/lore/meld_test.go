package lore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/mathomhaus/guild/internal/command"
	"github.com/mathomhaus/guild/internal/storage"
)

func TestMeld_ExactDup_CrossProject(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "proj-a", "proj-b")

	sharedTitle := "Agents must appraise before inscribing to avoid duplicate lore"
	sharedSummary := "Always search existing lore before writing new entries to prevent knowledge bloat."

	// Insert identical entry in proj-a.
	_, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('proj-a', 'test', 'principle', ?, ?, 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		sharedTitle, sharedSummary,
	)
	if err != nil {
		t.Fatalf("insert proj-a: %v", err)
	}

	// Insert identical entry in proj-b.
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('proj-b', 'test', 'principle', ?, ?, 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		sharedTitle, sharedSummary,
	)
	if err != nil {
		t.Fatalf("insert proj-b: %v", err)
	}

	// threshold=1.0 → exact-only pass; no near-match scan.
	pairs, err := Meld(ctx, db, 1.0, true, "")
	if err != nil {
		t.Fatalf("Meld: %v", err)
	}

	if len(pairs) != 1 {
		t.Fatalf("len(pairs) = %d; want 1", len(pairs))
	}

	p := pairs[0]
	if p.Score != 1.0 {
		t.Errorf("Score = %v; want 1.0 (exact match)", p.Score)
	}
	if p.LeftProject == p.RightProject {
		t.Errorf("expected cross-project pair; got same project %q on both sides", p.LeftProject)
	}
	if p.KindDrift {
		t.Errorf("KindDrift = true; want false (both are principle)")
	}
}

func TestMeld_KindDrift_Detected(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "proj-a", "proj-b")

	sharedTitle := "Cross project lore deduplication prevents knowledge fragmentation"
	sharedSummary := "Dedup detects identical entries across projects."

	// proj-a: kind=principle
	_, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('proj-a', 'test', 'principle', ?, ?, 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		sharedTitle, sharedSummary,
	)
	if err != nil {
		t.Fatalf("insert proj-a principle: %v", err)
	}

	// proj-b: kind=observation — kind drift
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('proj-b', 'test', 'observation', ?, ?, 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		sharedTitle, sharedSummary,
	)
	if err != nil {
		t.Fatalf("insert proj-b observation: %v", err)
	}

	pairs, err := Meld(ctx, db, 1.0, true, "")
	if err != nil {
		t.Fatalf("Meld: %v", err)
	}

	if len(pairs) != 1 {
		t.Fatalf("len(pairs) = %d; want 1", len(pairs))
	}
	if !pairs[0].KindDrift {
		t.Errorf("KindDrift = false; want true (principle vs observation)")
	}
}

func TestMeld_NearMatch(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "proj-a", "proj-b")

	// Two highly similar but not identical entries.
	titleA := "Agents must always appraise the lore before inscribing new knowledge entries"
	summaryA := "Searching existing lore before writing prevents duplicate artifacts accumulation."

	titleB := "AI agents should appraise the lore before inscribing knowledge to avoid redundancy"
	summaryB := "Searching lore before inscribing prevents duplicate entries from accumulating."

	_, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('proj-a', 'test', 'principle', ?, ?, 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		titleA, summaryA,
	)
	if err != nil {
		t.Fatalf("insert proj-a: %v", err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('proj-b', 'test', 'principle', ?, ?, 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		titleB, summaryB,
	)
	if err != nil {
		t.Fatalf("insert proj-b: %v", err)
	}

	// threshold=0.4 should catch this near-match (measured Jaccard ≈ 0.48).
	pairs, err := Meld(ctx, db, 0.4, true, "")
	if err != nil {
		t.Fatalf("Meld near-match: %v", err)
	}

	if len(pairs) == 0 {
		t.Error("Meld returned 0 pairs; expected at least 1 near-match pair")
	}
	for _, p := range pairs {
		if p.Score > 1.0 || p.Score < 0 {
			t.Errorf("Score out of range: %v", p.Score)
		}
	}
}

func TestMeld_ExcludesNonCurrent(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "proj-a", "proj-b")

	sharedTitle := "Identical knowledge stored in multiple projects causes confusion"
	sharedSummary := "Duplicate lore entries create contradictions."

	// proj-a: status=current
	_, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('proj-a', 'test', 'principle', ?, ?, 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		sharedTitle, sharedSummary,
	)
	if err != nil {
		t.Fatalf("insert proj-a: %v", err)
	}
	// proj-b: status=archived → should be excluded
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('proj-b', 'test', 'principle', ?, ?, 'archived', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		sharedTitle, sharedSummary,
	)
	if err != nil {
		t.Fatalf("insert proj-b archived: %v", err)
	}

	pairs, err := Meld(ctx, db, 1.0, true, "")
	if err != nil {
		t.Fatalf("Meld: %v", err)
	}

	if len(pairs) != 0 {
		t.Errorf("len(pairs) = %d; want 0 (archived entry excluded)", len(pairs))
	}
}

func TestMeld_Empty(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "empty")

	pairs, err := Meld(ctx, db, 1.0, true, "")
	if err != nil {
		t.Fatalf("Meld on empty db: %v", err)
	}
	if len(pairs) != 0 {
		t.Errorf("len(pairs) = %d; want 0", len(pairs))
	}
}

// openMeldFileDB creates a file-backed lore DB, seeds two projects with the
// provided entries, and returns the file path. Callers open fresh connections
// via storage.Open so the handler's defer-Close doesn't affect test state.
func openMeldFileDB(t *testing.T, entries []struct {
	pid, title, summary string
}) string {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "meld.db")
	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	if err := storage.MigrateTo(ctx, db, "test", nil); err != nil {
		_ = db.Close()
		t.Fatalf("MigrateTo: %v", err)
	}
	seenProjects := map[string]bool{}
	for _, e := range entries {
		if !seenProjects[e.pid] {
			if _, err := db.ExecContext(ctx,
				`INSERT INTO projects (id, path) VALUES (?, ?)`, e.pid, "/fake/"+e.pid,
			); err != nil {
				_ = db.Close()
				t.Fatalf("insert project %q: %v", e.pid, err)
			}
			seenProjects[e.pid] = true
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
			 VALUES (?, 'test', 'principle', ?, ?, 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			e.pid, e.title, e.summary,
		); err != nil {
			_ = db.Close()
			t.Fatalf("insert entry: %v", err)
		}
	}
	_ = db.Close()
	return path
}

// meldDepsForPath builds a command.Deps whose OpenDB opens a fresh connection
// to dbPath on each call — safe for handlers that defer-Close their handle.
func meldDepsForPath(dbPath string) command.Deps {
	return command.Deps{
		OpenDB: func(ctx context.Context) (*sql.DB, error) {
			return storage.Open(ctx, dbPath)
		},
		ResolveProj: func(_ context.Context, _ string) (string, error) {
			return "", nil
		},
	}
}

// TestMeldCmd_DefaultThreshold_ExactOnly is the command-layer regression
// guard. It verifies that calling the handler with an empty MeldInput (no
// threshold field) defaults to exact-only mode — it must NOT surface
// near-matches. The Go zero value for float64 is 0.0, which would pass every
// pair through the O(n²) near-match scan; the handler must set 1.0 explicitly
// when the caller omits the flag.
func TestMeldCmd_DefaultThreshold_ExactOnly(t *testing.T) {
	ctx := context.Background()

	exactTitle := "Agents must appraise lore before inscribing to avoid duplicate knowledge"
	exactSummary := "Always search existing lore before writing new entries to prevent knowledge bloat."

	nearTitleA := "Agents should always appraise lore before inscribing new knowledge entries to the store"
	nearSummaryA := "Searching existing lore before writing prevents duplicate artifacts accumulation."
	nearTitleB := "AI agents should appraise the lore before inscribing knowledge to avoid redundancy"
	nearSummaryB := "Searching lore before inscribing prevents duplicate entries from accumulating."

	dbPath := openMeldFileDB(t, []struct{ pid, title, summary string }{
		{"proj-a", exactTitle, exactSummary},
		{"proj-b", exactTitle, exactSummary}, // exact dup of proj-a
		{"proj-c", nearTitleA, nearSummaryA}, // near-match of proj-d, not exact
		{"proj-d", nearTitleB, nearSummaryB},
	})
	deps := meldDepsForPath(dbPath)

	// Zero-value MeldInput — no threshold set; handler must default to 1.0.
	out, err := MeldCommand.Handler(ctx, deps, MeldInput{})
	if err != nil {
		t.Fatalf("MeldCommand.Handler: %v", err)
	}

	// Only the exact dup pair should surface; near-match must be suppressed.
	if len(out.Pairs) != 1 {
		t.Fatalf("default threshold: got %d pair(s); want 1 (exact only)", len(out.Pairs))
	}
	if out.Pairs[0].Score != 1.0 {
		t.Errorf("pair score = %v; want 1.0 (exact match)", out.Pairs[0].Score)
	}
}

// TestMeldCmd_ExplicitThreshold_SurfacesNearMatch verifies the opt-in path:
// an explicit threshold below 1.0 must still surface near-matches.
func TestMeldCmd_ExplicitThreshold_SurfacesNearMatch(t *testing.T) {
	ctx := context.Background()

	nearTitleA := "Agents should always appraise lore before inscribing new knowledge entries to the store"
	nearSummaryA := "Searching existing lore before writing prevents duplicate artifacts accumulation."
	nearTitleB := "AI agents should appraise the lore before inscribing knowledge to avoid redundancy"
	nearSummaryB := "Searching lore before inscribing prevents duplicate entries from accumulating."

	dbPath := openMeldFileDB(t, []struct{ pid, title, summary string }{
		{"proj-e", nearTitleA, nearSummaryA},
		{"proj-f", nearTitleB, nearSummaryB},
	})
	deps := meldDepsForPath(dbPath)

	out, err := MeldCommand.Handler(ctx, deps, MeldInput{Threshold: "0.4"})
	if err != nil {
		t.Fatalf("MeldCommand.Handler explicit threshold: %v", err)
	}

	if len(out.Pairs) == 0 {
		t.Error("explicit threshold=0.4: got 0 pairs; expected at least 1 near-match")
	}
}
