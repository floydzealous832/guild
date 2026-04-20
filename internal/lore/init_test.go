package lore

import (
	"context"
	"errors"
	"testing"

	"github.com/mathomhaus/guild/internal/project"
)

// TestInit_RegistersProject stubs the git toplevel to a controlled
// path and verifies project.Register landed the row.
func TestInit_RegistersProject(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	original := gitToplevelFn
	t.Cleanup(func() { gitToplevelFn = original })
	gitToplevelFn = func(_ context.Context, _ string) (string, error) {
		return "/fake/path/to/my-test-project", nil
	}

	res, err := Init(ctx, db)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if res.Name != "my-test-project" {
		t.Errorf("want my-test-project, got %q", res.Name)
	}
	if res.Path != "/fake/path/to/my-test-project" {
		t.Errorf("unexpected path: %q", res.Path)
	}

	p, err := project.LookupByName(ctx, db, "my-test-project")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if p.Path != "/fake/path/to/my-test-project" {
		t.Errorf("registered path wrong: %q", p.Path)
	}
}

// TestInit_Idempotent — calling Init twice is a successful no-op.
func TestInit_Idempotent(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	original := gitToplevelFn
	t.Cleanup(func() { gitToplevelFn = original })
	gitToplevelFn = func(_ context.Context, _ string) (string, error) {
		return "/fake/path/reinit", nil
	}

	if _, err := Init(ctx, db); err != nil {
		t.Fatalf("init 1: %v", err)
	}
	if _, err := Init(ctx, db); err != nil {
		t.Fatalf("init 2 (should be no-op): %v", err)
	}
}

// TestInit_NotInGitRepo produces the ErrNotInGitRepo error.
func TestInit_NotInGitRepo(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	original := gitToplevelFn
	t.Cleanup(func() { gitToplevelFn = original })
	gitToplevelFn = func(_ context.Context, _ string) (string, error) {
		return "", ErrNotInGitRepo
	}

	_, err := Init(ctx, db)
	if !errors.Is(err, ErrNotInGitRepo) {
		t.Errorf("want ErrNotInGitRepo, got %v", err)
	}
}
