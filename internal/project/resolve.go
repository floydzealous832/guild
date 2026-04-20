package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotInGitRepo is returned when --project is empty AND the caller's
// working directory is not inside a git work tree. Callers can branch
// on this via errors.Is to emit a CLI-friendly error.
var ErrNotInGitRepo = errors.New("not inside a git repository")

// Resolver is the test-override surface for Resolve. Production code
// never instantiates one — the package-level DefaultResolver is used
// via Resolve(...). Tests construct their own Resolver with injected
// Getwd + GitToplevel functions so they don't have to chdir and don't
// have to spawn real `git` subprocesses.
//
// Keep this struct small: every field is a clear, testable seam, and
// adding fields should require a spec reason. The three seams here map
// 1:1 to the sources of ambient-state nondeterminism in resolve:
// the shell's CWD, the git work-tree toplevel, and the git common-dir
// (needed for worktree-aware resolution).
type Resolver struct {
	// Getwd returns the caller's working directory. Defaults to os.Getwd.
	Getwd func() (string, error)
	// GitToplevel returns the absolute path to the git work-tree root
	// that contains dir, or ErrNotInGitRepo if dir is not inside a repo.
	// Defaults to running `git rev-parse --show-toplevel` with
	// exec.CommandContext so cancellation propagates.
	GitToplevel func(ctx context.Context, dir string) (string, error)
	// GitCommonDir returns the path to the common git directory for the
	// repo that contains dir. For the main working tree this is the same
	// as the .git directory; for a linked worktree it points back to the
	// main repo's .git directory (e.g. /main/repo/.git). Defaults to
	// running `git rev-parse --git-common-dir` with exec.CommandContext.
	// Used to fall back to the main-repo path when the worktree path is
	// not registered.
	GitCommonDir func(ctx context.Context, dir string) (string, error)
}

// ResolveResult carries the project and a flag that indicates how it
// was resolved. ViaWorktreeFallback is true when the worktree path was
// not registered but the main-repo path was — callers can vary
// narration accordingly.
type ResolveResult struct {
	Project             *Project
	ViaWorktreeFallback bool
}

// DefaultResolver is the Resolver production code uses via Resolve.
// Kept as a variable (not a constant func) so test scaffolding can
// temporarily swap it out if ever needed; day-to-day test code
// constructs its own Resolver{} literal and calls its methods instead.
var DefaultResolver = Resolver{
	Getwd:        os.Getwd,
	GitToplevel:  gitToplevel,
	GitCommonDir: gitCommonDir,
}

// Resolve implements the CLI project-resolution order:
//
//  1. explicit --project flag (non-empty) → look up by name
//  2. else: `git rev-parse --show-toplevel` of CWD → look up by path
//  3. else (worktree fallback): `git rev-parse --git-common-dir` → derive
//     main-repo path → look up by path
//  4. else: return a structured error with recovery guidance
//
// NO silent fallback. An unregistered project or a not-in-git-repo CWD
// is a terminal error the caller prints and exits on. Resolve returns
// the error and the caller chooses how to surface it — cobra's RunE,
// the MCP error shape, etc.
//
// The `flag` parameter is the value of --project (empty means not set).
func Resolve(ctx context.Context, db *sql.DB, flag string) (*Project, error) {
	res, err := DefaultResolver.ResolveFull(ctx, db, flag)
	if err != nil {
		return nil, err
	}
	return res.Project, nil
}

// ResolveFull is the package-level form of Resolver.ResolveFull.
func ResolveFull(ctx context.Context, db *sql.DB, flag string) (ResolveResult, error) {
	return DefaultResolver.ResolveFull(ctx, db, flag)
}

// Resolve is the method form used by tests that need to inject Getwd
// or GitToplevel. Production code calls the package-level Resolve.
// It delegates to ResolveFull and strips the worktree-fallback flag.
func (r Resolver) Resolve(ctx context.Context, db *sql.DB, flag string) (*Project, error) {
	res, err := r.ResolveFull(ctx, db, flag)
	if err != nil {
		return nil, err
	}
	return res.Project, nil
}

// ResolveFull is the worktree-aware resolution method. It returns a
// ResolveResult that includes the Project and a ViaWorktreeFallback
// flag indicating whether resolution succeeded only because of the
// git-common-dir fallback (i.e., the cwd is a linked worktree whose
// main-repo path IS registered but the worktree path itself is not).
func (r Resolver) ResolveFull(ctx context.Context, db *sql.DB, flag string) (ResolveResult, error) {
	if db == nil {
		return ResolveResult{}, fmt.Errorf("project: resolve: nil db")
	}

	// Step 1: explicit flag wins. An explicit --project that's NOT
	// registered is still an error (no silent fallback to git), but
	// the error message points at the flag, not the CWD.
	if name := strings.TrimSpace(flag); name != "" {
		p, err := LookupByName(ctx, db, name)
		if err == nil {
			return ResolveResult{Project: p}, nil
		}
		if errors.Is(err, ErrNotRegistered) {
			return ResolveResult{}, formatUnregistered(ctx, db, name,
				fmt.Sprintf("project %q (from --project) not registered", name))
		}
		return ResolveResult{}, err
	}

	// Step 2: derive from git toplevel of CWD.
	getwd := r.Getwd
	if getwd == nil {
		getwd = os.Getwd
	}
	cwd, err := getwd()
	if err != nil {
		return ResolveResult{}, fmt.Errorf("project: resolve: getwd: %w", err)
	}

	git := r.GitToplevel
	if git == nil {
		git = gitToplevel
	}
	toplevel, err := git(ctx, cwd)
	if err != nil {
		if errors.Is(err, ErrNotInGitRepo) {
			return ResolveResult{}, fmt.Errorf(
				"not inside a git repository and no --project given — "+
					"run 'guild init' from the project root or pass --project <name> "+
					"(cwd: %s)", cwd)
		}
		return ResolveResult{}, fmt.Errorf("project: resolve: git toplevel of %s: %w", cwd, err)
	}

	p, err := LookupByPath(ctx, db, toplevel)
	if err == nil {
		return ResolveResult{Project: p}, nil
	}
	if !errors.Is(err, ErrNotRegistered) {
		return ResolveResult{}, err
	}

	// Step 3: worktree fallback. The worktree path isn't registered —
	// check whether we're inside a linked worktree by querying the
	// git common-dir. If the common-dir differs from the worktree's own
	// .git, we're in a worktree; strip "/.git" to get the main-repo root
	// and retry LookupByPath.
	commonDir := r.GitCommonDir
	if commonDir == nil {
		commonDir = gitCommonDir
	}
	cd, cdErr := commonDir(ctx, cwd)
	if cdErr == nil && cd != toplevel+"/.git" && cd != toplevel {
		// We're in a linked worktree. Derive the main-repo path by
		// stripping the trailing "/.git" suffix from the common-dir.
		mainRepoPath := strings.TrimSuffix(cd, "/.git")
		if mainRepoPath != cd { // only proceed when suffix was present
			mp, mpErr := LookupByPath(ctx, db, mainRepoPath)
			if mpErr == nil {
				return ResolveResult{Project: mp, ViaWorktreeFallback: true}, nil
			}
			if errors.Is(mpErr, ErrNotRegistered) {
				// Neither path registered — surface both in error.
				base := filepath.Base(toplevel)
				return ResolveResult{}, formatUnregistered(ctx, db, base,
					fmt.Sprintf("project %q not registered — tried worktree path %s and main-repo path %s — run 'guild init' first",
						base, toplevel, mainRepoPath))
			}
			return ResolveResult{}, mpErr
		}
	}

	// Step 4: neither worktree fallback applied nor path registered.
	base := filepath.Base(toplevel)
	return ResolveResult{}, formatUnregistered(ctx, db, base,
		fmt.Sprintf("project %q not registered — run 'guild init' first (path: %s)",
			base, toplevel))
}

// formatUnregistered enriches an unregistered error with the list of
// currently-registered projects so the caller sees both the failure
// and the available alternatives in one message. If listing fails we
// fall back to the bare message rather than swallowing the original
// problem under a secondary error.
func formatUnregistered(ctx context.Context, db *sql.DB, _ /*name*/, base string) error {
	projs, listErr := List(ctx, db)
	if listErr != nil || len(projs) == 0 {
		return fmt.Errorf("%s: %w", base, ErrNotRegistered)
	}
	ids := make([]string, 0, len(projs))
	for _, p := range projs {
		ids = append(ids, p.ID)
	}
	return fmt.Errorf("%s (registered: %s): %w",
		base, strings.Join(ids, ", "), ErrNotRegistered)
}

// gitToplevel runs `git rev-parse --show-toplevel` in dir and returns
// the absolute path to the git work-tree root, or ErrNotInGitRepo if
// dir is not inside a repository. Uses exec.CommandContext so tool
// cancellation reaches the git subprocess.
func gitToplevel(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	// Intentionally don't capture stderr into the error message — git's
	// stderr leaks absolute paths from unrelated tooling and confuses
	// agents reading the error. The exit-code check below is enough.
	out, err := cmd.Output()
	if err != nil {
		// git exits non-zero if dir is not inside a repo. We can't
		// reliably distinguish "not a repo" from "git missing" from
		// exit codes alone across git versions, so we probe: if git
		// isn't on PATH at all, return that distinctly; otherwise
		// assume not-in-repo.
		if _, lookErr := exec.LookPath("git"); lookErr != nil {
			return "", fmt.Errorf("project: git not found on PATH: %w", lookErr)
		}
		return "", ErrNotInGitRepo
	}
	return strings.TrimSpace(string(out)), nil
}

// gitCommonDir runs `git rev-parse --git-common-dir` in dir and returns
// the path to the common git directory. For the main working tree this
// equals the .git directory itself; for a linked worktree it points back
// to the main repo's .git (e.g. /main/repo/.git). The returned path may
// be relative (".git") when called from the main working tree — callers
// handling the worktree case compare with toplevel+"/.git" so both forms
// are handled.
func gitCommonDir(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-common-dir")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if _, lookErr := exec.LookPath("git"); lookErr != nil {
			return "", fmt.Errorf("project: git not found on PATH: %w", lookErr)
		}
		return "", ErrNotInGitRepo
	}
	result := strings.TrimSpace(string(out))
	// git may return a relative path (".git") when in the main working tree.
	// Make it absolute so callers can do path comparisons.
	if !filepath.IsAbs(result) {
		result = filepath.Join(dir, result)
	}
	return result, nil
}
