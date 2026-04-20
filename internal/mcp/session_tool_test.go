package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/mathomhaus/guild/internal/project"
	"github.com/mathomhaus/guild/internal/quest"
)

// withStubbedResolver swaps mcpProjectResolver for the duration of a
// test so inferProjectFromCWD can be exercised without touching the
// real filesystem or spawning git. Getwd returns cwd verbatim;
// gitToplevelErr is the error GitToplevel reports (nil = success, in
// which case it returns cwd as the toplevel). GitCommonDir defaults to
// returning cwd+"/.git" (i.e., not a worktree).
func withStubbedResolver(t *testing.T, cwd string, gitToplevelErr error) {
	t.Helper()
	saved := mcpProjectResolver
	t.Cleanup(func() { mcpProjectResolver = saved })
	mcpProjectResolver = project.Resolver{
		Getwd: func() (string, error) { return cwd, nil },
		GitToplevel: func(ctx context.Context, dir string) (string, error) {
			if gitToplevelErr != nil {
				return "", gitToplevelErr
			}
			return dir, nil
		},
		// Default: cwd is its own main repo (not a worktree).
		GitCommonDir: func(ctx context.Context, dir string) (string, error) {
			return dir + "/.git", nil
		},
	}
}

// TestInferProjectFromCWD_RegisteredPath exercises the happy path:
// cwd resolves to a path that's registered in the projects table.
func TestInferProjectFromCWD_RegisteredPath(t *testing.T) {
	isolateHome(t)
	ctx := context.Background()

	const (
		pid     = "proj-a"
		projDir = "/fake/workspaces/proj-a"
	)

	db, err := openQuestDB(ctx)
	if err != nil {
		t.Fatalf("open quest db: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := project.Register(ctx, db, pid, projDir, "TASKS.md"); err != nil {
		t.Fatalf("project.Register: %v", err)
	}

	withStubbedResolver(t, projDir, nil)

	got, _, err := inferProjectFromCWD(ctx)
	if err != nil {
		t.Fatalf("inferProjectFromCWD: %v", err)
	}
	if got != pid {
		t.Errorf("inferred project = %q, want %q", got, pid)
	}
}

// TestInferProjectFromCWD_NotInGitRepo asserts the clear error shape
// when the MCP server's cwd isn't inside a git work tree. The agent's
// recovery path is to pass project=... explicitly.
func TestInferProjectFromCWD_NotInGitRepo(t *testing.T) {
	isolateHome(t)
	ctx := context.Background()

	withStubbedResolver(t, "/tmp/not-a-repo", project.ErrNotInGitRepo)

	_, _, err := inferProjectFromCWD(ctx)
	if err == nil {
		t.Fatal("inferProjectFromCWD returned nil error; want not-in-git-repo")
	}
	if !strings.Contains(err.Error(), "not inside a git repository") {
		t.Errorf("error %q missing not-in-repo guidance", err.Error())
	}
}

// TestInferProjectFromCWD_PathNotRegistered asserts the error when the
// cwd resolves to a valid git toplevel whose path isn't registered.
// The resolver's message already lists registered alternatives.
func TestInferProjectFromCWD_PathNotRegistered(t *testing.T) {
	isolateHome(t)
	ctx := context.Background()

	// Register proj-a at one path, then ask inference to resolve a
	// different path that isn't registered.
	const registered = "proj-a"
	db, err := openQuestDB(ctx)
	if err != nil {
		t.Fatalf("open quest db: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := project.Register(ctx, db, registered, "/some/path/proj-a", "TASKS.md"); err != nil {
		t.Fatalf("project.Register: %v", err)
	}

	withStubbedResolver(t, "/different/path/unknown", nil)

	_, _, inferErr := inferProjectFromCWD(ctx)
	if inferErr == nil {
		t.Fatal("inferProjectFromCWD returned nil error; want not-registered")
	}
	if !strings.Contains(inferErr.Error(), "not registered") {
		t.Errorf("error %q missing not-registered guidance", inferErr.Error())
	}
	// Registered project names should be surfaced so the agent knows
	// what it could pass explicitly.
	if !strings.Contains(inferErr.Error(), registered) {
		t.Errorf("error %q should list registered project %q", inferErr.Error(), registered)
	}
}

// TestHandleSessionStart_ExplicitProjectSkipsInference asserts that an
// explicit in.Project wins — the resolver isn't even consulted. Seeds
// the resolver with a panic-on-call stub to prove it stays untouched.
func TestHandleSessionStart_ExplicitProjectSkipsInference(t *testing.T) {
	isolateHome(t)
	ctx := context.Background()

	const pid = "explicit-proj"
	db, err := openQuestDB(ctx)
	if err != nil {
		t.Fatalf("open quest db: %v", err)
	}
	_ = project.Register(ctx, db, pid, "/some/path", "TASKS.md")
	_ = db.Close()

	saved := mcpProjectResolver
	t.Cleanup(func() { mcpProjectResolver = saved })
	mcpProjectResolver = project.Resolver{
		Getwd: func() (string, error) {
			t.Fatal("Getwd called despite explicit project arg — inference should have been skipped")
			return "", nil
		},
		GitToplevel: func(context.Context, string) (string, error) {
			t.Fatal("GitToplevel called despite explicit project arg")
			return "", nil
		},
		GitCommonDir: func(context.Context, string) (string, error) {
			t.Fatal("GitCommonDir called despite explicit project arg")
			return "", nil
		},
	}

	res, _, callErr := handleSessionStart(ctx, nil, sessionStartInput{Project: pid})
	if callErr != nil {
		t.Fatalf("handleSessionStart: %v", callErr)
	}
	if res.IsError {
		t.Errorf("expected success, got IsError with content: %v", res.Content)
	}
}

// TestHandleSessionStart_InferenceSucceeds asserts the end-to-end
// empty-arg path: no project arg given, resolver returns a registered
// path, handler succeeds and sets the active project.
func TestHandleSessionStart_InferenceSucceeds(t *testing.T) {
	isolateHome(t)
	ctx := context.Background()

	const (
		pid     = "inferred-proj"
		projDir = "/fake/workspaces/inferred-proj"
	)

	db, err := openQuestDB(ctx)
	if err != nil {
		t.Fatalf("open quest db: %v", err)
	}
	if err := project.Register(ctx, db, pid, projDir, "TASKS.md"); err != nil {
		t.Fatalf("project.Register: %v", err)
	}
	_ = db.Close()

	withStubbedResolver(t, projDir, nil)

	res, _, callErr := handleSessionStart(ctx, nil, sessionStartInput{Project: ""})
	if callErr != nil {
		t.Fatalf("handleSessionStart: %v", callErr)
	}
	if res.IsError {
		t.Fatalf("inferred-path handler returned IsError; content: %v", res.Content)
	}
	// The narration header echoes back the resolved project id so the
	// user sees which project auto-infer picked.
	body := textOf(res.Content)
	if !strings.Contains(body, pid) {
		t.Errorf("response body %q missing resolved project id %q", body, pid)
	}
}

// TestHandleSessionStart_InferenceFailureIsRecoverable asserts that
// when inference fails, the handler returns a recoverable error
// (IsError=true, not a protocol error) and the message names the
// explicit-arg escape hatch.
func TestHandleSessionStart_InferenceFailureIsRecoverable(t *testing.T) {
	isolateHome(t)
	ctx := context.Background()

	withStubbedResolver(t, "/tmp/unregistered", project.ErrNotInGitRepo)

	res, _, callErr := handleSessionStart(ctx, nil, sessionStartInput{Project: ""})
	if callErr != nil {
		t.Fatalf("handleSessionStart must not return a protocol error (agent cannot recover): %v", callErr)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true on inference failure")
	}
	body := textOf(res.Content)
	// The handler's wrapped guidance must tell the agent how to unblock.
	for _, want := range []string{
		"auto-inference from cwd failed",
		"project='<directory-name>'",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("failure body missing recovery guidance %q; got:\n%s", want, body)
		}
	}
}

// withStubbedWorktreeResolver swaps mcpProjectResolver to simulate a
// linked-worktree scenario: the worktree path (worktreePath) is NOT
// registered; the main-repo path (mainRepoPath) IS. GitToplevel returns
// the worktree path; GitCommonDir returns mainRepoPath+"/.git".
func withStubbedWorktreeResolver(t *testing.T, worktreePath, mainRepoPath string) {
	t.Helper()
	saved := mcpProjectResolver
	t.Cleanup(func() { mcpProjectResolver = saved })
	mcpProjectResolver = project.Resolver{
		Getwd: func() (string, error) { return worktreePath, nil },
		GitToplevel: func(_ context.Context, dir string) (string, error) {
			return worktreePath, nil
		},
		GitCommonDir: func(_ context.Context, dir string) (string, error) {
			return mainRepoPath + "/.git", nil
		},
	}
}

// TestHandleSessionStart_WorktreeFallbackNarration asserts that when
// auto-inference resolves via the git-common-dir worktree fallback, the
// response header contains the fallback suffix so the agent and user see
// which resolution path was taken.
func TestHandleSessionStart_WorktreeFallbackNarration(t *testing.T) {
	isolateHome(t)
	ctx := context.Background()

	const (
		pid          = "main-proj"
		mainRepoPath = "/fake/projects/main-proj"
		worktreePath = "/fake/worktrees/main-proj-feat"
	)

	db, err := openQuestDB(ctx)
	if err != nil {
		t.Fatalf("open quest db: %v", err)
	}
	// Register only the main-repo path, not the worktree path.
	if err := project.Register(ctx, db, pid, mainRepoPath, "TASKS.md"); err != nil {
		t.Fatalf("project.Register: %v", err)
	}
	_ = db.Close()

	withStubbedWorktreeResolver(t, worktreePath, mainRepoPath)

	res, _, callErr := handleSessionStart(ctx, nil, sessionStartInput{Project: ""})
	if callErr != nil {
		t.Fatalf("handleSessionStart: %v", callErr)
	}
	if res.IsError {
		t.Fatalf("expected success, got IsError with content: %v", res.Content)
	}
	body := textOf(res.Content)
	// The narration header must name the project.
	if !strings.Contains(body, pid) {
		t.Errorf("response body %q missing project id %q", body, pid)
	}
	// The worktree-fallback suffix must be present.
	if !strings.Contains(body, "inferred from worktree's main-repo path") {
		t.Errorf("response body %q missing worktree-fallback narration", body)
	}
}

// TestFormatBounties_EmptyStateRendersAllSections is QUEST-1's
// regression guard. When session_start is called on a project with no
// briefing, no oath, no echoes, and no bounties, every structural
// section header must still appear so a first-time user sees the
// shape of what they're about to fill in — not an unhelpful
// one-liner "no unclaimed tasks".
func TestFormatBounties_EmptyStateRendersAllSections(t *testing.T) {
	// Empty result with NoUnclaimed=true simulates a fresh project
	// where quest.Bounties found no unclaimed tasks, no briefing,
	// no oath, no echoes.
	res := &quest.BountiesResult{NoUnclaimed: true}
	body := formatBounties(res, false)

	wantMarkers := []string{
		"📋 last briefing",
		"⚔️ oath",
		"👻 fading echoes",
		"🎯 bounties",
		"⚡ parallelism",
	}
	for _, marker := range wantMarkers {
		if !strings.Contains(body, marker) {
			t.Errorf("empty-state body missing section marker %q; got:\n%s",
				marker, body)
		}
	}
}

// TestFormatBounties_PopulatedSectionsStillRender checks that when
// real data is present, the corresponding section displays it (the
// headers don't wipe out content).
func TestFormatBounties_PopulatedSectionsStillRender(t *testing.T) {
	res := &quest.BountiesResult{
		LastBriefAgent: "agent-a",
		LastBriefAt:    "2026-04-17T12:00",
		LastBriefText:  "work in progress",
		Oath: []quest.OathEntry{
			{Title: "be nice", Summary: "treat the code well"},
		},
		Echoes: []quest.EchoEntry{
			{Title: "old note", Reason: "30d stale"},
		},
	}
	body := formatBounties(res, false)

	for _, want := range []string{
		"work in progress",
		"be nice",
		"old note",
		"🎯 bounties: (none yet)", // no TopQuest, no NoUnclaimed — fallback branch
	} {
		if !strings.Contains(body, want) {
			t.Errorf("populated body missing %q; got:\n%s", want, body)
		}
	}
}

// TestEmptyBountiesSkeleton_AllSections checks the absolute-cold-start
// fallback (quest DB unavailable) also surfaces the five sections so
// the user experience is consistent whether the DB is reachable or not.
func TestEmptyBountiesSkeleton_AllSections(t *testing.T) {
	body := emptyBountiesSkeleton()
	for _, marker := range []string{
		"📋 last briefing",
		"⚔️ oath",
		"👻 fading echoes",
		"🎯 bounties",
		"⚡ parallelism",
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("skeleton missing %q; got:\n%s", marker, body)
		}
	}
}
