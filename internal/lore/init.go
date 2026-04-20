package lore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mathomhaus/guild/internal/project"
)

// InitResult carries what `lore init` just did so the CLI layer can print
// a friendly confirmation line (name + absolute path).
type InitResult struct {
	Name string
	Path string
}

// ErrNotInGitRepo is returned when Init is called outside a git work tree
// and no explicit path override has been supplied.
var ErrNotInGitRepo = errors.New("not inside a git repository")

// gitToplevelFn is the seam for shelling out to git. Tests replace it
// with a canned path so they don't need a real git repo.
var gitToplevelFn = defaultGitToplevel

// SwapGitToplevelForTest swaps the package's git-toplevel resolver and
// returns the previous value. Tests (including external-package CLI
// tests) use this to plug in a canned toplevel. Not part of the v1
// public API; intended as a test seam only.
func SwapGitToplevelForTest(fn func(ctx context.Context, cwd string) (string, error)) func(context.Context, string) (string, error) {
	prev := gitToplevelFn
	gitToplevelFn = fn
	return prev
}

// Init registers the current directory's git toplevel as a project in
// the shared `projects` table. The project id is the directory basename
// (so /Users/foo/projects/guild → "guild"); the path column stores the
// absolute toplevel path. Re-running Init is idempotent — the underlying
// project.Register upserts on the primary key.
//
// If cwd is "" the function uses exec to resolve git's toplevel from the
// process's own working directory. Tests pass an explicit cwd via the
// exported variant InitFrom.
func Init(ctx context.Context, db *sql.DB) (*InitResult, error) {
	return InitFrom(ctx, db, "")
}

// InitFrom is the test-facing variant of Init that lets callers override
// the starting directory. cwd="" falls back to os.Getwd inside git. All
// other behavior matches Init.
func InitFrom(ctx context.Context, db *sql.DB, cwd string) (*InitResult, error) {
	if db == nil {
		return nil, fmt.Errorf("lore: init: nil db")
	}

	toplevel, err := gitToplevelFn(ctx, cwd)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(toplevel)
	if name == "" || name == "." || name == "/" {
		return nil, fmt.Errorf("lore: init: cannot derive project name from %q", toplevel)
	}

	if err := project.Register(ctx, db, name, toplevel, ""); err != nil {
		return nil, fmt.Errorf("lore: init: register %q: %w", name, err)
	}
	return &InitResult{Name: name, Path: toplevel}, nil
}

// defaultGitToplevel runs `git rev-parse --show-toplevel` against cwd
// (or the process's own CWD when cwd is empty) and returns the absolute
// path. Uses exec.CommandContext so host cancellation reaches the
// subprocess.
func defaultGitToplevel(ctx context.Context, cwd string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	if cwd != "" {
		cmd.Dir = cwd
	}
	out, err := cmd.Output()
	if err != nil {
		if _, lookErr := exec.LookPath("git"); lookErr != nil {
			return "", fmt.Errorf("lore: init: git not found on PATH: %w", lookErr)
		}
		return "", ErrNotInGitRepo
	}
	return strings.TrimSpace(string(out)), nil
}
