package lore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchive_BasicRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "arch-proj")

	// Insert 2 entries.
	_, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('arch-proj', 'test', 'research', 'Research Finding One', 'First finding.', 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert entry 1: %v", err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('arch-proj', 'test', 'decision', 'Architecture Decision', 'Chose X.', 'current', '2026-01-02T00:00:00Z', '2026-01-02T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert entry 2: %v", err)
	}

	snapshotPath := filepath.Join(t.TempDir(), "snapshot.json")
	if err := Archive(ctx, db, "arch-proj", snapshotPath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// File must exist.
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}

	// Parse and assert structure.
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}

	if snap.SchemaVersion != SnapshotVersion {
		t.Errorf("schema_version = %d; want %d", snap.SchemaVersion, SnapshotVersion)
	}
	if snap.SnapshotAt == "" {
		t.Error("snapshot_at is empty")
	}
	if snap.ProducedBy == "" {
		t.Error("produced_by is empty")
	}
	if len(snap.Lore) != 2 {
		t.Errorf("len(lore) = %d; want 2", len(snap.Lore))
	}

	// Validate snapshot_at is parseable as RFC3339.
	if _, parseErr := time.Parse(time.RFC3339, snap.SnapshotAt); parseErr != nil {
		t.Errorf("snapshot_at not RFC3339: %v", parseErr)
	}
}

func TestArchive_PreservesQuestSection(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "arch-quest-proj")

	// Pre-seed a snapshot.json with quest data.
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "snapshot.json")

	questPayload := json.RawMessage(`[{"id":"QUEST-1","subject":"Test quest"}]`)
	initial := snapshot{
		SchemaVersion: 1,
		SnapshotAt:    "2026-01-01T00:00:00Z",
		ProducedBy:    "guild 0.1.0",
		Lore:          []snapshotLoreEntry{},
		Quest:         questPayload,
	}
	initialData, _ := json.MarshalIndent(initial, "", "  ")
	initialData = append(initialData, '\n')
	if err := os.WriteFile(snapshotPath, initialData, 0o600); err != nil {
		t.Fatalf("write initial snapshot: %v", err)
	}

	// Insert a lore entry and run Archive.
	_, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('arch-quest-proj', 'test', 'principle', 'A principle', 'Short.', 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert entry: %v", err)
	}

	if err := Archive(ctx, db, "arch-quest-proj", snapshotPath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Read back and assert both lore AND quest sections are present.
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot after archive: %v", err)
	}

	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Lore section should have our 1 entry.
	if len(snap.Lore) != 1 {
		t.Errorf("len(lore) = %d; want 1", len(snap.Lore))
	}

	// Quest section must be preserved unchanged.
	if snap.Quest == nil {
		t.Fatal("quest section is nil; expected the pre-seeded quest data")
	}

	var questArr []map[string]interface{}
	if err := json.Unmarshal(snap.Quest, &questArr); err != nil {
		t.Fatalf("unmarshal quest: %v", err)
	}
	if len(questArr) != 1 {
		t.Errorf("quest array len = %d; want 1", len(questArr))
	}
	if questArr[0]["id"] != "QUEST-1" {
		t.Errorf("quest[0].id = %v; want QUEST-1", questArr[0]["id"])
	}
}

func TestArchive_AtomicWrite(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "arch-atomic")

	// Ensure no leftover .tmp file after a successful Archive.
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "snapshot.json")

	if err := Archive(ctx, db, "arch-atomic", snapshotPath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	tmpPath := snapshotPath + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Errorf(".tmp file should have been removed after successful archive: %s", tmpPath)
	}
}

func TestArchive_EmptyProject(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "arch-empty")

	snapshotPath := filepath.Join(t.TempDir(), "snapshot.json")
	if err := Archive(ctx, db, "arch-empty", snapshotPath); err != nil {
		t.Fatalf("Archive on empty project: %v", err)
	}

	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}

	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if snap.SchemaVersion != SnapshotVersion {
		t.Errorf("schema_version = %d; want %d", snap.SchemaVersion, SnapshotVersion)
	}
	if snap.Lore == nil {
		t.Error("lore should be [] not null for empty project")
	}
}

// TestArchive_PreservesLinks exercises the full Archive → Restore round-trip
// with provenance edges. This is the regression guard: before the fix, Archive
// emitted no links key, so Restore always returned LinksAdded=0 regardless of
// what was in entry_links.
func TestArchive_PreservesLinks(t *testing.T) {
	ctx := context.Background()
	// Two distinct project IDs: src is archived, dst is the restore target.
	db := openTestDB(t, "arch-links-src", "arch-links-dst")

	// Insert two entries in the source project.
	res1, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('arch-links-src', 'test', 'research', 'Source entry', 'Finding.', 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert entry 1: %v", err)
	}
	id1, _ := res1.LastInsertId()

	res2, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('arch-links-src', 'test', 'decision', 'Decision entry', 'Chose Y.', 'current', '2026-01-02T00:00:00Z', '2026-01-02T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert entry 2: %v", err)
	}
	id2, _ := res2.LastInsertId()

	// Wire a provenance edge: entry 1 informs entry 2.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO entry_links (from_id, to_id, relation) VALUES (?, ?, 'informs')`,
		id1, id2,
	); err != nil {
		t.Fatalf("insert link: %v", err)
	}

	snapshotPath := filepath.Join(t.TempDir(), "snapshot.json")
	if err := Archive(ctx, db, "arch-links-src", snapshotPath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// The snapshot must contain the links section.
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(snap.Links) != 1 {
		t.Fatalf("len(links) = %d; want 1", len(snap.Links))
	}
	if snap.Links[0].Relation != "informs" {
		t.Errorf("link relation = %q; want informs", snap.Links[0].Relation)
	}

	// Restore into destination project and verify the edge is rehydrated.
	result, err := Restore(ctx, db, "arch-links-dst", snapshotPath)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if result.Imported != 2 {
		t.Errorf("Imported = %d; want 2", result.Imported)
	}
	if result.LinksAdded != 1 {
		t.Errorf("LinksAdded = %d; want 1", result.LinksAdded)
	}

	// Confirm entry_links row landed in the DB for the destination project.
	var linkCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entry_links
		  WHERE from_id IN (SELECT id FROM entries WHERE project_id = 'arch-links-dst')`,
	).Scan(&linkCount); err != nil {
		t.Fatalf("count restored links: %v", err)
	}
	if linkCount != 1 {
		t.Errorf("restored entry_links count = %d; want 1", linkCount)
	}
}

// TestArchive_DanglingEdgesOmitted verifies that when archiving a project
// that has a cross-project link (one endpoint outside the archived set),
// the dangling edge is silently dropped — it would point nowhere on restore.
func TestArchive_DanglingEdgesOmitted(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "arch-dangle-a", "arch-dangle-b")

	// Entry in project A.
	resA, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('arch-dangle-a', 'test', 'research', 'Entry A', 'In project A.', 'current', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert entry A: %v", err)
	}
	idA, _ := resA.LastInsertId()

	// Entry in project B (outside the archive scope).
	resB, err := db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('arch-dangle-b', 'test', 'decision', 'Entry B', 'In project B.', 'current', '2026-01-02T00:00:00Z', '2026-01-02T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert entry B: %v", err)
	}
	idB, _ := resB.LastInsertId()

	// Cross-project edge: A informs B.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO entry_links (from_id, to_id, relation) VALUES (?, ?, 'informs')`,
		idA, idB,
	); err != nil {
		t.Fatalf("insert cross-project link: %v", err)
	}

	// Archive only project A — the edge must be excluded because B is not in snapshot.
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.json")
	if err := Archive(ctx, db, "arch-dangle-a", snapshotPath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(snap.Links) != 0 {
		t.Errorf("len(links) = %d; want 0 (dangling cross-project edge must be dropped)", len(snap.Links))
	}
}
