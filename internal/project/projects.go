// Package project owns the `projects` registry table and the CLI-side
// project resolution order.
//
// Every guild command needs to know which project it operates on. For CLI
// invocations that answer comes from (in order):
//
//  1. an explicit `--project NAME` flag,
//  2. `git rev-parse --show-toplevel` of CWD looked up in the `projects`
//     table,
//  3. a clean error with recovery guidance — never a silent fallback.
//
// The MCP-side resolution order (arg → per-PID session file → env) lives
// in `internal/session`; the two packages are deliberately split because
// the MCP server runs in a subprocess that can't see the caller's CWD.
//
// All exported functions take a `ctx context.Context` and use parameterized
// SQL. No `fmt.Sprintf` into Query/Exec is allowed.
package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Project is a row in the `projects` table.
//
// The schema (from migrations/001_init.up.sql) is:
//
//	id         TEXT PRIMARY KEY      — directory basename, e.g. "guild"
//	path       TEXT UNIQUE NOT NULL  — absolute filesystem path
//	tasks_file TEXT NOT NULL         — defaults to "TASKS.md"
//	created_at TEXT                  — datetime('now')
type Project struct {
	ID        string
	Path      string
	TasksFile string
	CreatedAt string
}

// ErrNotRegistered is returned by LookupByName / LookupByPath when no
// row matches. Callers use errors.Is to distinguish "not registered"
// from other DB errors so they can print the right recovery guidance.
var ErrNotRegistered = errors.New("project not registered")

// Register inserts a project row, or updates path/tasks_file on conflict
// with the same id (idempotent upsert). Callers that want strictly-create
// semantics should LookupByName first.
//
// name is the project id (directory basename). path must be absolute.
// tasksFile may be empty; the table's DEFAULT 'TASKS.md' fills it in
// when the column is omitted, but we always pass a non-empty value to
// keep the write deterministic (empty string collapses to the default
// here rather than at the DB boundary).
func Register(ctx context.Context, db *sql.DB, name, path, tasksFile string) error {
	if db == nil {
		return fmt.Errorf("project: register: nil db")
	}
	if name = strings.TrimSpace(name); name == "" {
		return fmt.Errorf("project: register: empty name")
	}
	if path = strings.TrimSpace(path); path == "" {
		return fmt.Errorf("project: register: empty path")
	}
	if tasksFile = strings.TrimSpace(tasksFile); tasksFile == "" {
		tasksFile = "TASKS.md"
	}

	// Upsert so `Register` is idempotent. The ON CONFLICT target is the
	// primary key (id); path and tasks_file refresh on re-register.
	// Parameterized — no Sprintf into the SQL string.
	_, err := db.ExecContext(ctx,
		`INSERT INTO projects (id, path, tasks_file)
		 VALUES (?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   path       = excluded.path,
		   tasks_file = excluded.tasks_file`,
		name, path, tasksFile,
	)
	if err != nil {
		return fmt.Errorf("project: register %q: %w", name, err)
	}
	return nil
}

// LookupByName returns the project row whose id equals name, or
// ErrNotRegistered if no such row exists.
func LookupByName(ctx context.Context, db *sql.DB, name string) (*Project, error) {
	if db == nil {
		return nil, fmt.Errorf("project: lookup by name: nil db")
	}
	if name = strings.TrimSpace(name); name == "" {
		return nil, fmt.Errorf("project: lookup by name: empty name")
	}

	row := db.QueryRowContext(ctx,
		`SELECT id, path, tasks_file, COALESCE(created_at, '')
		 FROM projects WHERE id = ?`,
		name,
	)
	p := &Project{}
	if err := row.Scan(&p.ID, &p.Path, &p.TasksFile, &p.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNotRegistered, name)
		}
		return nil, fmt.Errorf("project: lookup by name %q: %w", name, err)
	}
	return p, nil
}

// LookupByPath returns the project row whose filesystem path equals
// path (exact match — callers are expected to pass the git-toplevel of
// their CWD, which is canonicalized). Returns ErrNotRegistered if no
// row matches.
func LookupByPath(ctx context.Context, db *sql.DB, path string) (*Project, error) {
	if db == nil {
		return nil, fmt.Errorf("project: lookup by path: nil db")
	}
	if path = strings.TrimSpace(path); path == "" {
		return nil, fmt.Errorf("project: lookup by path: empty path")
	}

	row := db.QueryRowContext(ctx,
		`SELECT id, path, tasks_file, COALESCE(created_at, '')
		 FROM projects WHERE path = ?`,
		path,
	)
	p := &Project{}
	if err := row.Scan(&p.ID, &p.Path, &p.TasksFile, &p.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNotRegistered, path)
		}
		return nil, fmt.Errorf("project: lookup by path %q: %w", path, err)
	}
	return p, nil
}

// List returns every registered project sorted by id ascending. Used by
// CLI error messages (`registered projects: foo, bar, baz`) and by
// future `guild doctor` style diagnostics.
func List(ctx context.Context, db *sql.DB) ([]Project, error) {
	if db == nil {
		return nil, fmt.Errorf("project: list: nil db")
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, path, tasks_file, COALESCE(created_at, '')
		 FROM projects ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("project: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Path, &p.TasksFile, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("project: list: scan: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("project: list: iterate: %w", err)
	}
	return out, nil
}
