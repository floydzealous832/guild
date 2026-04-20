package quest

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestArchive_Basic(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	q := mustPost(t, db, pid, PostParams{Subject: "snap me"})

	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "snapshot.json")

	if err := Archive(ctx, db, pid, snapshotPath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Verify the file exists and parses.
	raw, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var snap snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if snap.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", snap.SchemaVersion)
	}
	if snap.Quest == nil {
		t.Fatal("quest section nil")
	}
	if len(snap.Quest.TaskStatus) != 1 {
		t.Fatalf("want 1 task_status, got %d", len(snap.Quest.TaskStatus))
	}
	if snap.Quest.TaskStatus[0].TaskID != q.ID {
		t.Errorf("TaskID = %q, want %q", snap.Quest.TaskStatus[0].TaskID, q.ID)
	}
	// lore section defaults to [] when file didn't exist.
	if string(snap.Lore) != "[]" {
		t.Errorf("lore = %s, want []", snap.Lore)
	}
}

func TestArchive_PreservesLoreSection(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	mustPost(t, db, pid, PostParams{Subject: "task"})

	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "snapshot.json")

	// Pre-seed snapshot.json with a lore section.
	loreSeed := `[{"id":"ENTRY-1","title":"a lore entry","kind":"decision","summary":"test"}]`
	existing := map[string]interface{}{
		"schema_version": 1,
		"snapshot_at":    "2026-01-01T00:00:00Z",
		"produced_by":    "test",
		"lore":           json.RawMessage(loreSeed),
		"quest":          nil,
	}
	seed, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(snapshotPath, append(seed, '\n'), 0o600); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	// Run archive — should preserve the lore section.
	if err := Archive(ctx, db, pid, snapshotPath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Read back and verify lore is preserved.
	raw, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var snap snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Lore should be exactly the seed (not []).
	// Parse both to compare as JSON values, not raw bytes.
	var gotLore, wantLore interface{}
	if err := json.Unmarshal(snap.Lore, &gotLore); err != nil {
		t.Fatalf("unmarshal got lore: %v", err)
	}
	if err := json.Unmarshal([]byte(loreSeed), &wantLore); err != nil {
		t.Fatalf("unmarshal want lore: %v", err)
	}

	gotBytes, _ := json.Marshal(gotLore)
	wantBytes, _ := json.Marshal(wantLore)
	if !bytes.Equal(gotBytes, wantBytes) {
		t.Errorf("lore section changed:\ngot  %s\nwant %s", gotBytes, wantBytes)
	}

	// Quest section should have been updated.
	if snap.Quest == nil || len(snap.Quest.TaskStatus) != 1 {
		t.Errorf("quest section not updated: %+v", snap.Quest)
	}
}

func TestArchive_AtomicRename(t *testing.T) {
	// Verify no .tmp file is left behind after a successful archive.
	db, pid := newTestDB(t)
	ctx := context.Background()

	mustPost(t, db, pid, PostParams{Subject: "task"})

	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "snapshot.json")

	if err := Archive(ctx, db, pid, snapshotPath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	tmp := snapshotPath + ".tmp"
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("tmp file should not exist after successful archive: %v", err)
	}
}

func TestArchive_DefaultPath(t *testing.T) {
	// Archive with empty snapshotPath resolves from project registration.
	db, pid := newTestDB(t)
	ctx := context.Background()

	// The project was registered with a TempDir path in newTestDB.
	// We need to look up that path to verify the output.
	var projPath string
	if err := db.QueryRowContext(ctx,
		`SELECT path FROM projects WHERE id = ?`, pid,
	).Scan(&projPath); err != nil {
		t.Fatalf("lookup path: %v", err)
	}

	mustPost(t, db, pid, PostParams{Subject: "auto-path"})

	if err := Archive(ctx, db, pid, ""); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	defaultPath := filepath.Join(projPath, ".guild", "snapshot.json")
	if _, err := os.Stat(defaultPath); err != nil {
		t.Errorf("snapshot not created at %s: %v", defaultPath, err)
	}
}
