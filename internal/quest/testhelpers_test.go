package quest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/mathomhaus/guild/internal/storage"
)

// newTestDB opens a per-test file-backed SQLite DB under t.TempDir,
// applies migrations, and registers a dummy project. Returns the open
// *sql.DB and the project_id.
//
// We use a tmpdir file (not :memory:) because the storage package
// rejects query-string DSNs, and a plain ":memory:" DB doesn't share
// across pooled connections — modernc.org/sqlite would give each goroutine
// in the race test a distinct empty DB. A tmpdir file with WAL is the
// simplest way to exercise real concurrency against the driver.
func newTestDB(t *testing.T) (db *sql.DB, projectID string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "quest.db")
	var err error
	db, err = storage.Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if migrateErr := storage.Migrate(ctx, db, ""); migrateErr != nil {
		t.Fatalf("migrate: %v", migrateErr)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO projects (id, path, tasks_file) VALUES (?, ?, ?)`,
		"testproj", t.TempDir(), "TASKS.md",
	); err != nil {
		t.Fatalf("register project: %v", err)
	}
	projectID = "testproj"
	return
}

// mustStatus returns the status column for taskID, failing the test on
// any error.
func mustStatus(t *testing.T, db *sql.DB, pid, taskID string) Status {
	t.Helper()
	var s sql.NullString
	err := db.QueryRowContext(context.Background(),
		`SELECT status FROM task_status WHERE project_id = ? AND task_id = ?`,
		pid, taskID,
	).Scan(&s)
	if err != nil {
		t.Fatalf("mustStatus %s: %v", taskID, err)
	}
	return Status(s.String)
}

// mustLoad returns the full Quest, failing the test on any error.
func mustLoad(t *testing.T, db *sql.DB, pid, taskID string) *Quest {
	t.Helper()
	q, err := Load(context.Background(), db, pid, taskID)
	if err != nil {
		t.Fatalf("Load %s: %v", taskID, err)
	}
	return q
}

// mustPost posts a quest with the given subject & params.
//
//nolint:gocritic // hugeParam: test helper mirrors Post's public signature
func mustPost(t *testing.T, db *sql.DB, pid string, params PostParams) *Quest {
	t.Helper()
	q, err := Post(context.Background(), db, pid, params)
	if err != nil {
		t.Fatalf("Post %q: %v", params.Subject, err)
	}
	return q
}
