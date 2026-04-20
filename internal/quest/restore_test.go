package quest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRestore_Basic(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	// Archive a quest.
	q := mustPost(t, db, pid, PostParams{Subject: "snapshot task"})
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "snapshot.json")
	if err := Archive(ctx, db, pid, snapshotPath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Create a fresh DB and restore.
	db2, pid2 := newTestDB(t)
	result, err := Restore(ctx, db2, pid2, snapshotPath)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if result.TasksImported != 1 {
		t.Errorf("TasksImported = %d, want 1", result.TasksImported)
	}

	// Verify quest exists in new DB.
	loaded := mustLoad(t, db2, pid2, q.ID)
	if loaded.Subject != q.Subject {
		t.Errorf("subject = %q, want %q", loaded.Subject, q.Subject)
	}
}

func TestRestore_Idempotent(t *testing.T) {
	// Restoring twice should not duplicate task_status rows.
	db, pid := newTestDB(t)
	ctx := context.Background()

	mustPost(t, db, pid, PostParams{Subject: "task"})
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "snapshot.json")
	if err := Archive(ctx, db, pid, snapshotPath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	db2, pid2 := newTestDB(t)
	r1, err := Restore(ctx, db2, pid2, snapshotPath)
	if err != nil {
		t.Fatalf("Restore 1: %v", err)
	}
	r2, err := Restore(ctx, db2, pid2, snapshotPath)
	if err != nil {
		t.Fatalf("Restore 2: %v", err)
	}
	if r1.TasksImported != 1 {
		t.Errorf("first restore: want 1 task, got %d", r1.TasksImported)
	}
	if r2.TasksImported != 0 {
		t.Errorf("second restore: want 0 tasks (idempotent), got %d", r2.TasksImported)
	}
}

func TestRestore_SchemaVersionGate(t *testing.T) {
	// schema_version: 99 should return an error.
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "snapshot.json")

	bad := map[string]interface{}{
		"schema_version": 99,
		"snapshot_at":    "2026-01-01T00:00:00Z",
		"lore":           []interface{}{},
		"quest":          nil,
	}
	raw, _ := json.Marshal(bad)
	if err := os.WriteFile(snapshotPath, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	db, pid := newTestDB(t)
	_, err := Restore(context.Background(), db, pid, snapshotPath)
	if err == nil {
		t.Error("want error for unsupported schema_version, got nil")
	}
}

func TestRestore_MissingFile(t *testing.T) {
	db, pid := newTestDB(t)
	_, err := Restore(context.Background(), db, pid, "/nonexistent/snapshot.json")
	if err == nil {
		t.Error("want error for missing file, got nil")
	}
}

func TestRestore_NoQuestSection(t *testing.T) {
	// A snapshot with only lore and no quest section is valid — returns empty result.
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "snapshot.json")

	snap := map[string]interface{}{
		"schema_version": 1,
		"snapshot_at":    "2026-01-01T00:00:00Z",
		"lore":           []interface{}{},
	}
	raw, _ := json.Marshal(snap)
	if err := os.WriteFile(snapshotPath, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	db, pid := newTestDB(t)
	result, err := Restore(context.Background(), db, pid, snapshotPath)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if result.TasksImported != 0 {
		t.Errorf("TasksImported = %d, want 0", result.TasksImported)
	}
}

func TestRestorePreservesLore(t *testing.T) {
	// archive → restore round-trip: lore section from a pre-seeded snapshot
	// survives in the on-disk file (archive preserves it; restore doesn't touch it).
	db, pid := newTestDB(t)
	ctx := context.Background()

	mustPost(t, db, pid, PostParams{Subject: "task"})

	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "snapshot.json")

	// Seed lore section.
	loreSeed := `[{"id":"ENTRY-1","title":"test lore","kind":"decision","summary":"test"}]`
	existing := map[string]interface{}{
		"schema_version": 1,
		"snapshot_at":    "2026-01-01T00:00:00Z",
		"produced_by":    "test",
		"lore":           json.RawMessage(loreSeed),
		"quest":          nil,
	}
	seed, _ := json.Marshal(existing)
	if err := os.WriteFile(snapshotPath, seed, 0o600); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	// Archive preserves lore.
	if err := Archive(ctx, db, pid, snapshotPath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Read back — lore should still be there.
	raw, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var snap snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	var loreItems []interface{}
	if err := json.Unmarshal(snap.Lore, &loreItems); err != nil {
		t.Fatalf("parse lore: %v", err)
	}
	if len(loreItems) != 1 {
		t.Errorf("lore items = %d, want 1 (lore section lost on archive)", len(loreItems))
	}

	// Now restore into a fresh DB.
	db2, pid2 := newTestDB(t)
	result, err := Restore(ctx, db2, pid2, snapshotPath)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if result.TasksImported != 1 {
		t.Errorf("TasksImported = %d, want 1", result.TasksImported)
	}
}
