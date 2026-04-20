package lore

import (
	"context"
	"strings"
	"testing"
)

// shortTitle builds a title with exactly n words.
func buildWords(n int) string {
	words := make([]string, n)
	for i := range words {
		words[i] = "word"
	}
	return strings.Join(words, " ")
}

func TestInquest_Categorization(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "proj-a")

	// Helper: insert a principle with given word count into proj-a.
	insertPrinciple := func(title, summary string) {
		t.Helper()
		_, err := db.ExecContext(ctx,
			`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
			 VALUES ('proj-a', 'test', 'principle', ?, ?, 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			title, summary,
		)
		if err != nil {
			t.Fatalf("insert principle: %v", err)
		}
	}

	// Short (≤30 words total): 3 entries.
	// 10-word title + empty summary = 10 words.
	insertPrinciple(buildWords(10), "")
	insertPrinciple(buildWords(15), "")
	insertPrinciple(buildWords(30), "") // exactly 30 → still short

	// Medium (31-60 words total): 3 entries.
	// 20-word title + 15-word summary = 35 words.
	insertPrinciple(buildWords(20), buildWords(15))
	// 30-word title + 5-word summary = 35 words.
	insertPrinciple(buildWords(30), buildWords(5))
	// 40-word title + 20-word summary = 60 words (boundary — exactly bloatBoundary, so medium).
	insertPrinciple(buildWords(40), buildWords(20))

	// Bloat (>60 words total): 3 entries.
	// 40-word title + 25-word summary = 65 words.
	insertPrinciple(buildWords(40), buildWords(25))
	// 50-word title + 20-word summary = 70 words.
	insertPrinciple(buildWords(50), buildWords(20))
	// 60-word title + 30-word summary = 90 words.
	insertPrinciple(buildWords(60), buildWords(30))

	result, err := Inquest(ctx, db, "proj-a", false, 60)
	if err != nil {
		t.Fatalf("Inquest: %v", err)
	}

	if result.TotalOaths != 9 {
		t.Errorf("TotalOaths = %d; want 9", result.TotalOaths)
	}

	if len(result.Projects) != 1 {
		t.Fatalf("len(Projects) = %d; want 1", len(result.Projects))
	}

	stats := result.Projects[0]
	if stats.Short != 3 {
		t.Errorf("Short = %d; want 3", stats.Short)
	}
	if stats.Medium != 3 {
		t.Errorf("Medium = %d; want 3", stats.Medium)
	}
	if stats.Bloat != 3 {
		t.Errorf("Bloat = %d; want 3", stats.Bloat)
	}

	// BloatEntries should be sorted descending by word count.
	if len(result.BloatEntries) != 3 {
		t.Fatalf("len(BloatEntries) = %d; want 3", len(result.BloatEntries))
	}
	for i := 1; i < len(result.BloatEntries); i++ {
		if result.BloatEntries[i-1].WordCount < result.BloatEntries[i].WordCount {
			t.Errorf("BloatEntries not sorted desc: index %d (%d) < index %d (%d)",
				i-1, result.BloatEntries[i-1].WordCount, i, result.BloatEntries[i].WordCount)
		}
	}
}

func TestInquest_AllProjects(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "proj-x", "proj-y")

	insert := func(pid, title, summary string) {
		t.Helper()
		_, err := db.ExecContext(ctx,
			`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
			 VALUES (?, 'test', 'principle', ?, ?, 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			pid, title, summary,
		)
		if err != nil {
			t.Fatalf("insert principle pid=%q: %v", pid, err)
		}
	}

	// proj-x: 2 short, 1 bloat
	insert("proj-x", buildWords(5), "")
	insert("proj-x", buildWords(10), "")
	insert("proj-x", buildWords(70), "")

	// proj-y: 1 medium, 1 bloat
	insert("proj-y", buildWords(40), "")
	insert("proj-y", buildWords(80), "")

	result, err := Inquest(ctx, db, "", true, 60)
	if err != nil {
		t.Fatalf("Inquest allProjects: %v", err)
	}

	if result.TotalOaths != 5 {
		t.Errorf("TotalOaths = %d; want 5", result.TotalOaths)
	}
	if len(result.Projects) != 2 {
		t.Fatalf("len(Projects) = %d; want 2", len(result.Projects))
	}
	if len(result.BloatEntries) != 2 {
		t.Errorf("len(BloatEntries) = %d; want 2", len(result.BloatEntries))
	}

	// Verify the project ordering is lexicographic.
	if result.Projects[0].ProjectID != "proj-x" {
		t.Errorf("Projects[0].ProjectID = %q; want proj-x", result.Projects[0].ProjectID)
	}
	if result.Projects[1].ProjectID != "proj-y" {
		t.Errorf("Projects[1].ProjectID = %q; want proj-y", result.Projects[1].ProjectID)
	}
}

func TestInquest_NoPrinciples(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "empty")

	result, err := Inquest(ctx, db, "empty", false, 60)
	if err != nil {
		t.Fatalf("Inquest empty project: %v", err)
	}

	if result.TotalOaths != 0 {
		t.Errorf("TotalOaths = %d; want 0", result.TotalOaths)
	}
	if len(result.BloatEntries) != 0 {
		t.Errorf("len(BloatEntries) = %d; want 0", len(result.BloatEntries))
	}
}

func TestInquest_ExcludesNonCurrent(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "scope")

	// Insert one bloat principle with status=archived — should be excluded.
	_, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('scope', 'test', 'principle', ?, '', 'archived', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		buildWords(80),
	)
	if err != nil {
		t.Fatalf("insert archived principle: %v", err)
	}
	// Insert one current principle.
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('scope', 'test', 'principle', ?, '', 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		buildWords(10),
	)
	if err != nil {
		t.Fatalf("insert current principle: %v", err)
	}

	result, err := Inquest(ctx, db, "scope", false, 60)
	if err != nil {
		t.Fatalf("Inquest: %v", err)
	}
	if result.TotalOaths != 1 {
		t.Errorf("TotalOaths = %d; want 1 (archived excluded)", result.TotalOaths)
	}
	if len(result.BloatEntries) != 0 {
		t.Errorf("len(BloatEntries) = %d; want 0 (archived excluded)", len(result.BloatEntries))
	}
}
