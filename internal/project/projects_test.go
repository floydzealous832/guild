package project

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/mathomhaus/guild/internal/storage"
)

// openTempDB spins up a disk-backed SQLite DB and applies the canonical
// 001_init migration so the `projects` table exists. Mirrors the helper
// in internal/storage/db_test.go but lives here to avoid a test-package
// dependency cycle. Takes ctx so contextcheck is satisfied through the
// call chain and the helper honors the caller's cancellation policy.
func openTempDB(ctx context.Context, t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := storage.MigrateTo(ctx, db, "test", nil); err != nil {
		t.Fatalf("storage.Migrate: %v", err)
	}
	return db
}

func TestRegister_InsertsRow(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	if err := Register(ctx, db, "guild", "/tmp/guild", ""); err != nil {
		t.Fatalf("Register: %v", err)
	}

	p, err := LookupByName(ctx, db, "guild")
	if err != nil {
		t.Fatalf("LookupByName: %v", err)
	}
	if p.ID != "guild" || p.Path != "/tmp/guild" || p.TasksFile != "TASKS.md" {
		t.Fatalf("unexpected row: %+v", p)
	}
}

func TestRegister_UpsertsPathAndTasksFile(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	if err := Register(ctx, db, "guild", "/old/path", "OLD.md"); err != nil {
		t.Fatalf("Register#1: %v", err)
	}
	if err := Register(ctx, db, "guild", "/new/path", "NEW.md"); err != nil {
		t.Fatalf("Register#2: %v", err)
	}

	p, err := LookupByName(ctx, db, "guild")
	if err != nil {
		t.Fatalf("LookupByName: %v", err)
	}
	if p.Path != "/new/path" || p.TasksFile != "NEW.md" {
		t.Fatalf("upsert lost data: %+v", p)
	}
}

func TestRegister_RejectsEmptyName(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	if err := Register(ctx, db, "", "/tmp/x", ""); err == nil {
		t.Fatalf("expected error on empty name")
	}
}

func TestRegister_RejectsEmptyPath(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	if err := Register(ctx, db, "guild", "", ""); err == nil {
		t.Fatalf("expected error on empty path")
	}
}

func TestLookupByName_NotRegisteredReturnsSentinel(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	_, err := LookupByName(ctx, db, "ghost")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("want ErrNotRegistered, got %v", err)
	}
}

func TestLookupByPath_NotRegisteredReturnsSentinel(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	_, err := LookupByPath(ctx, db, "/tmp/no-such-project")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("want ErrNotRegistered, got %v", err)
	}
}

func TestLookupByPath_RoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	if err := Register(ctx, db, "guild", "/tmp/guild-abs", ""); err != nil {
		t.Fatalf("Register: %v", err)
	}
	p, err := LookupByPath(ctx, db, "/tmp/guild-abs")
	if err != nil {
		t.Fatalf("LookupByPath: %v", err)
	}
	if p.ID != "guild" {
		t.Fatalf("want guild, got %q", p.ID)
	}
}

func TestList_SortedByID(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	// Insert out of order so sorting is verified, not incidental.
	for _, pair := range []struct {
		name, path string
	}{
		{"zeta", "/tmp/zeta"},
		{"alpha", "/tmp/alpha"},
		{"mike", "/tmp/mike"},
	} {
		if err := Register(ctx, db, pair.name, pair.path, ""); err != nil {
			t.Fatalf("Register %q: %v", pair.name, err)
		}
	}

	got, err := List(ctx, db)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d", len(got))
	}
	want := []string{"alpha", "mike", "zeta"}
	for i, w := range want {
		if got[i].ID != w {
			t.Fatalf("row %d: want %q, got %q", i, w, got[i].ID)
		}
	}
}

func TestList_EmptyTable(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	got, err := List(ctx, db)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 rows, got %d", len(got))
	}
}

func TestRegister_NilDB(t *testing.T) {
	if err := Register(context.Background(), nil, "x", "/x", ""); err == nil {
		t.Fatalf("expected nil-db error")
	}
}
