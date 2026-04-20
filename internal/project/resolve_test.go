package project

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// resolverCase describes one step through the resolution precedence
// table. Each field models a substitutable input so the whole
// precedence order can be table-tested without touching the real CWD,
// git, or the environment.
type resolverCase struct {
	name string

	flag     string // value of the --project flag
	cwd      string // simulated CWD (returned by Getwd seam)
	toplevel string // simulated `git rev-parse --show-toplevel` output
	gitErr   error  // simulated git-error (e.g. ErrNotInGitRepo)

	registered map[string]string // id -> path rows seeded before resolution

	wantID    string // expected Project.ID on success
	wantErr   bool   // expected error
	wantErrIs error  // expected errors.Is target, if wantErr
	// substrings the error message must contain. Gives us a stable
	// hook on the user-facing text without asserting the whole string.
	wantErrContains []string
}

func TestResolve_PrecedenceTable(t *testing.T) {
	ctx := context.Background()

	cases := []resolverCase{
		{
			name: "flag wins even when CWD would resolve elsewhere",
			flag: "guild",
			cwd:  "/tmp/otherwork",
			// git toplevel would succeed but is never consulted when flag set.
			toplevel:   "/tmp/otherproj",
			registered: map[string]string{"guild": "/tmp/guild", "otherproj": "/tmp/otherproj"},
			wantID:     "guild",
		},
		{
			name:       "flag unregistered is an error even in a valid git repo",
			flag:       "ghost",
			cwd:        "/tmp/work",
			toplevel:   "/tmp/guild",
			registered: map[string]string{"guild": "/tmp/guild"},
			wantErr:    true,
			wantErrIs:  ErrNotRegistered,
			wantErrContains: []string{
				"ghost",
				"--project",
				"registered: guild",
			},
		},
		{
			name:       "no flag + git-toplevel matches path",
			flag:       "",
			cwd:        "/tmp/work/subdir",
			toplevel:   "/tmp/guild",
			registered: map[string]string{"guild": "/tmp/guild"},
			wantID:     "guild",
		},
		{
			name:       "no flag + git-toplevel not registered",
			flag:       "",
			cwd:        "/tmp/work",
			toplevel:   "/tmp/unknown",
			registered: map[string]string{"guild": "/tmp/guild"},
			wantErr:    true,
			wantErrIs:  ErrNotRegistered,
			wantErrContains: []string{
				"unknown",
				"guild init",
				"/tmp/unknown",
			},
		},
		{
			name:       "no flag + not in a git repo",
			flag:       "",
			cwd:        "/tmp/elsewhere",
			gitErr:     ErrNotInGitRepo,
			registered: map[string]string{"guild": "/tmp/guild"},
			wantErr:    true,
			wantErrContains: []string{
				"not inside a git repository",
				"--project",
				"/tmp/elsewhere",
			},
		},
		{
			name:       "flag wins with leading/trailing whitespace trimmed",
			flag:       "  guild  ",
			registered: map[string]string{"guild": "/tmp/guild"},
			wantID:     "guild",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openTempDB(ctx, t)
			for name, path := range tc.registered {
				if err := Register(ctx, db, name, path, ""); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}

			r := Resolver{
				Getwd: func() (string, error) { return tc.cwd, nil },
				GitToplevel: func(_ context.Context, _ string) (string, error) {
					if tc.gitErr != nil {
						return "", tc.gitErr
					}
					return tc.toplevel, nil
				},
			}

			got, err := r.Resolve(ctx, db, tc.flag)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got project %+v", got)
				}
				if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("errors.Is(%v, %v) = false; err=%v",
						err, tc.wantErrIs, err)
				}
				for _, sub := range tc.wantErrContains {
					if !strings.Contains(err.Error(), sub) {
						t.Fatalf("err %q missing substring %q", err, sub)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil {
				t.Fatalf("want project, got nil")
			}
			if got.ID != tc.wantID {
				t.Fatalf("want ID %q, got %q", tc.wantID, got.ID)
			}
		})
	}
}

// TestResolve_NilDBIsError guards the one code path that doesn't touch
// the precedence table but should still refuse unsafely-constructed
// callers.
func TestResolve_NilDBIsError(t *testing.T) {
	if _, err := Resolve(context.Background(), nil, ""); err == nil {
		t.Fatalf("expected nil-db error")
	}
}

// TestResolve_GetwdErrorSurfaces checks that a Getwd failure propagates
// (not masked as "not in a git repo"). Rare in practice — permissions
// oddities on the shell's CWD — but the message must be honest so the
// user sees the real cause.
func TestResolve_GetwdErrorSurfaces(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	r := Resolver{
		Getwd:       func() (string, error) { return "", errors.New("permission denied") },
		GitToplevel: func(context.Context, string) (string, error) { return "", nil },
	}

	_, err := r.Resolve(ctx, db, "")
	if err == nil {
		t.Fatalf("want getwd error")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("want underlying error surfaced, got %v", err)
	}
}

// TestResolve_GitToplevelGenericErrorNotMaskedAsNotInRepo covers the
// contract boundary: only ErrNotInGitRepo triggers the friendly
// not-in-repo message; other errors (e.g. `git` missing from PATH) must
// stay distinct so the user diagnoses the real issue.
func TestResolve_GitToplevelGenericErrorNotMaskedAsNotInRepo(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	r := Resolver{
		Getwd: func() (string, error) { return "/tmp/work", nil },
		GitToplevel: func(context.Context, string) (string, error) {
			return "", errors.New("git: command not found")
		},
	}

	_, err := r.Resolve(ctx, db, "")
	if err == nil {
		t.Fatalf("want error")
	}
	if strings.Contains(err.Error(), "not inside a git repository") {
		t.Fatalf("generic error masked as not-in-repo: %v", err)
	}
}

// worktreeResolver builds a Resolver with injected seams simulating a
// git worktree environment. toplevel is the path git --show-toplevel
// returns (the worktree path); commonDir is what git --git-common-dir
// returns (the main repo's .git path).
func worktreeResolver(cwd, toplevel, commonDir string) Resolver {
	return Resolver{
		Getwd: func() (string, error) { return cwd, nil },
		GitToplevel: func(context.Context, string) (string, error) {
			return toplevel, nil
		},
		GitCommonDir: func(context.Context, string) (string, error) {
			return commonDir, nil
		},
	}
}

// TestWorktree_RegisteredMainRepo is a regression guard: cwd resolves
// directly to a registered main-repo path (no worktree involved).
// ViaWorktreeFallback must be false.
func TestWorktree_RegisteredMainRepo(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	const mainPath = "/tmp/main-repo"
	if err := Register(ctx, db, "main-repo", mainPath, ""); err != nil {
		t.Fatalf("Register: %v", err)
	}

	r := worktreeResolver(mainPath, mainPath, mainPath+"/.git")
	res, err := r.ResolveFull(ctx, db, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Project.ID != "main-repo" {
		t.Fatalf("want ID 'main-repo', got %q", res.Project.ID)
	}
	if res.ViaWorktreeFallback {
		t.Fatal("ViaWorktreeFallback should be false for direct main-repo match")
	}
}

// TestWorktree_WorktreeOfRegisteredRepo: the cwd is a linked worktree
// whose path is NOT registered, but the main-repo path IS. Resolution
// must succeed via the worktree fallback and ViaWorktreeFallback=true.
func TestWorktree_WorktreeOfRegisteredRepo(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	const (
		mainPath     = "/tmp/projects/guild"
		worktreePath = "/tmp/worktrees/guild-feat"
	)
	// Only register the main-repo path.
	if err := Register(ctx, db, "guild", mainPath, ""); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// git --show-toplevel returns worktree path; --git-common-dir returns
	// main repo's .git (absolute path ending in /.git).
	r := worktreeResolver(worktreePath+"/subdir", worktreePath, mainPath+"/.git")
	res, err := r.ResolveFull(ctx, db, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Project.ID != "guild" {
		t.Fatalf("want ID 'guild', got %q", res.Project.ID)
	}
	if !res.ViaWorktreeFallback {
		t.Fatal("ViaWorktreeFallback should be true when resolved via main-repo fallback")
	}
}

// TestWorktree_BothPathsRegistered: both the worktree path and the
// main-repo path are registered. The exact-match on the worktree path
// must win; ViaWorktreeFallback must be false (no fallback needed).
func TestWorktree_BothPathsRegistered(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	const (
		mainPath     = "/tmp/projects/guild"
		worktreePath = "/tmp/worktrees/guild-feat"
	)
	if err := Register(ctx, db, "guild", mainPath, ""); err != nil {
		t.Fatalf("Register main: %v", err)
	}
	if err := Register(ctx, db, "guild-feat", worktreePath, ""); err != nil {
		t.Fatalf("Register worktree: %v", err)
	}

	r := worktreeResolver(worktreePath, worktreePath, mainPath+"/.git")
	res, err := r.ResolveFull(ctx, db, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Project.ID != "guild-feat" {
		t.Fatalf("want ID 'guild-feat' (exact match wins), got %q", res.Project.ID)
	}
	if res.ViaWorktreeFallback {
		t.Fatal("ViaWorktreeFallback should be false when worktree path is directly registered")
	}
}

// TestWorktree_UnregisteredRepo: the cwd is in a worktree but neither
// the worktree path nor the main-repo path is registered. The error
// must mention BOTH paths so the agent sees what was tried.
func TestWorktree_UnregisteredRepo(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	const (
		mainPath     = "/tmp/projects/unregistered"
		worktreePath = "/tmp/worktrees/unregistered-feat"
		otherProject = "other-project"
	)
	// Register a different project so the error lists registered alternatives.
	if err := Register(ctx, db, otherProject, "/tmp/projects/other", ""); err != nil {
		t.Fatalf("Register other: %v", err)
	}

	r := worktreeResolver(worktreePath, worktreePath, mainPath+"/.git")
	_, err := r.ResolveFull(ctx, db, "")
	if err == nil {
		t.Fatal("want error for unregistered repo")
	}
	if !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("want ErrNotRegistered, got %v", err)
	}
	// Error must mention both the worktree path and the main-repo path.
	for _, path := range []string{worktreePath, mainPath} {
		if !strings.Contains(err.Error(), path) {
			t.Errorf("error %q should mention path %q", err.Error(), path)
		}
	}
}

// TestWorktree_NotInGitRepo: cwd is outside any git repo. The
// ErrNotInGitRepo sentinel must surface unchanged — the worktree
// code path is never reached.
func TestWorktree_NotInGitRepo(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(ctx, t)

	r := Resolver{
		Getwd:        func() (string, error) { return "/tmp/no-git", nil },
		GitToplevel:  func(context.Context, string) (string, error) { return "", ErrNotInGitRepo },
		GitCommonDir: func(context.Context, string) (string, error) { return "", ErrNotInGitRepo },
	}

	_, err := r.ResolveFull(ctx, db, "")
	if err == nil {
		t.Fatal("want error for not-in-git-repo")
	}
	if !strings.Contains(err.Error(), "not inside a git repository") {
		t.Fatalf("want not-in-git-repo message, got: %v", err)
	}
}
