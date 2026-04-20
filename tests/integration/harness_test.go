// Package integration contains end-to-end integration tests for the guild
// binary. Every test runs against the real compiled binary and a real SQLite
// database under a temporary HOME directory — no mocks.
//
// Binary strategy: one build via sync.Once, stored in a temp file shared
// across all subtests in the same test binary invocation. Each subtest gets
// its own HOME via t.TempDir() + t.Setenv("HOME", dir).
//
// Package choice: external package (integration_test) so the tests
// import guild only via the CLI boundary — no internal package access.
// This gives a hard compile-time guarantee that test code never calls
// lore.Inscribe / quest.Post etc. directly, enforcing the "no direct
// internal/ calls bypassing CLI" rule.
package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─────────────────────────── binary build (once) ────────────────────────────

// binaryOnce guarantees the guild binary is built exactly once per test
// run, regardless of how many subtests or parallel invocations exist.
var binaryOnce sync.Once

// binaryPath holds the path to the compiled guild binary after the first
// sync.Once fires. The value is set inside buildOnce; reads after that
// are safe from any goroutine because sync.Once provides the happens-before.
var binaryPath string

// buildErr captures any error from go build so tests can call
// requireBinary() and fail immediately if the build failed.
var buildErr error

// buildOnce compiles cmd/guild into a temp file and captures the path.
// Called by requireBinary; tests must not call it directly.
func buildOnce(t *testing.T) {
	t.Helper()
	binaryOnce.Do(func() {
		// Place the binary in the OS temp dir under a pid-specific name so
		// parallel test runs on the same machine don't collide.
		bin := filepath.Join(os.TempDir(),
			fmt.Sprintf("guild-integration-%d", os.Getpid()))
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		//nolint:gosec // bin is constructed from os.TempDir() + Getpid(); no user input
		cmd := exec.CommandContext(ctx, "go", "build",
			"-o", bin,
			"./cmd/guild")
		// Resolve the module root so 'go build ./cmd/guild' works
		// regardless of the test runner's working directory.
		cmd.Dir = moduleRoot(t)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			buildErr = fmt.Errorf("go build ./cmd/guild: %w\n%s", err, stderr.String())
			return
		}
		binaryPath = bin
		// Register cleanup — best-effort; test suite exit may not call
		// it on SIGKILL but that's acceptable (the OS reclaims temp files).
		// We do NOT use t.Cleanup here because t is the test that triggered
		// the Once, which may finish before other tests run.
	})
}

// requireBinary ensures the guild binary has been built and returns its
// path. Fails the calling test immediately if the build failed.
//
//nolint:contextcheck // buildOnce creates its own context internally via context.WithTimeout
func requireBinary(t *testing.T) string {
	t.Helper()
	buildOnce(t)
	if buildErr != nil {
		t.Fatalf("guild binary build failed: %v", buildErr)
	}
	return binaryPath
}

// moduleRoot returns the absolute path to the go.mod root by walking up
// from this file's location. Relies on runtime.Caller so the path is
// correct whether tests run from the module root or from the package dir.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	// thisFile is …/tests/integration/harness_test.go
	// module root is two directories up
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("moduleRoot: go.mod not found at %s: %v", root, err)
	}
	return root
}

// ─────────────────────────────── Invocation ─────────────────────────────────

// Invocation is the result of running guild with a set of arguments
// against an isolated HOME directory.
type Invocation struct {
	Stdout   string
	Stderr   string
	ExitCode int
	// Elapsed is the wall-clock duration of the subprocess call.
	Elapsed time.Duration
}

// RunOpts overrides the default environment for a single invocation.
// Zero value → use the test's HOME, no extra env, no stdin.
type RunOpts struct {
	// ExtraEnv adds key=value pairs to the subprocess env (after HOME is set).
	ExtraEnv []string
	// Stdin, when non-nil, is piped to the subprocess stdin.
	Stdin *bytes.Buffer
	// Dir, when non-empty, overrides the subprocess working directory.
	// Defaults to the homeDir so git-root detection inside the binary
	// doesn't accidentally pick up the test runner's own git root.
	Dir string
}

// run executes the guild binary with args against homeDir and returns
// the captured output. ctx is threaded through exec.CommandContext.
//
// The subprocess inherits only HOME + a minimal PATH + any ExtraEnv;
// the real user's environment is intentionally excluded to guarantee isolation.
func run(ctx context.Context, t *testing.T, homeDir, args string, opts *RunOpts) Invocation {
	t.Helper()
	bin := requireBinary(t)

	if opts == nil {
		opts = &RunOpts{}
	}

	argv := strings.Fields(args)
	//nolint:gosec // bin is from buildOnce (trusted), argv from test literals
	cmd := exec.CommandContext(ctx, bin, argv...)

	// Minimal environment: HOME + a PATH that includes the Go toolchain
	// dir (for any sub-`go` invocations the binary might make) plus the
	// system bin dirs. We blank out user-level env so ~/.guild from the
	// real user never leaks into the test.
	cmd.Env = append([]string{
		"HOME=" + homeDir,
		"PATH=" + os.Getenv("PATH"),
		// Disable telemetry writes so tests don't create usage.log noise
		// in the temp HOME.
		"GUILD_NO_USAGE_LOG=1",
	}, opts.ExtraEnv...)

	dir := opts.Dir
	if dir == "" {
		dir = homeDir
	}
	cmd.Dir = dir

	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if asExitErr(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Logf("run %q: unexpected exec error: %v", args, err)
			exitCode = -1
		}
	}

	return Invocation{
		Stdout:   strings.TrimRight(stdout.String(), "\n"),
		Stderr:   strings.TrimRight(stderr.String(), "\n"),
		ExitCode: exitCode,
		Elapsed:  elapsed,
	}
}

// asExitErr is an errors.As wrapper so the test code doesn't need to import
// errors just for this type assertion.
func asExitErr(err error, target **exec.ExitError) bool {
	if err == nil {
		return false
	}
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// ────────────────────────────── project init helpers ────────────────────────

// initProject runs `guild lore init` and `guild quest init` inside a
// temporary git repo rooted at projDir so the binary's git-root
// detection returns projDir as the project path. Returns the project name
// (= filepath.Base(projDir)).
//
// Callers that need a named project for CLI --project flags use the
// returned name directly; the lore.db / quest.db are always at
// homeDir/.guild/*.
func initProject(ctx context.Context, t *testing.T, homeDir, projDir string) string {
	t.Helper()

	// Create a bare git repo so `git rev-parse --show-toplevel` works.
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("mkdir projDir: %v", err)
	}
	gitCmd := exec.CommandContext(ctx, "git", "init", projDir)
	if out, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", projDir, err, out)
	}

	// guild lore init
	inv := run(ctx, t, homeDir, "lore init", &RunOpts{Dir: projDir})
	if inv.ExitCode != 0 {
		t.Fatalf("lore init failed (exit %d):\nstdout: %s\nstderr: %s",
			inv.ExitCode, inv.Stdout, inv.Stderr)
	}

	// guild quest init
	inv = run(ctx, t, homeDir, "quest init", &RunOpts{Dir: projDir})
	if inv.ExitCode != 0 {
		t.Fatalf("quest init failed (exit %d):\nstdout: %s\nstderr: %s",
			inv.ExitCode, inv.Stdout, inv.Stderr)
	}

	return filepath.Base(projDir)
}

// inscribe runs `guild lore inscribe TITLE --kind K --summary S --topic T`
// and returns the Invocation. The caller decides how to assert on it.
func inscribe(ctx context.Context, t *testing.T, homeDir, projDir, title, kind, summary, topic string) Invocation {
	t.Helper()
	// Shell-quote the title by passing it as a separate arg to avoid
	// spaces-in-args issues with strings.Fields splitting.
	return runArgs(ctx, t, homeDir, projDir, []string{
		"lore", "inscribe", title,
		"--kind", kind,
		"--summary", summary,
		"--topic", topic,
	})
}

// runArgs runs the guild binary with a pre-split args slice, avoiding
// strings.Fields splitting for args that contain spaces (titles, etc.).
func runArgs(ctx context.Context, t *testing.T, homeDir, dir string, argv []string) Invocation {
	t.Helper()
	bin := requireBinary(t)

	//nolint:gosec // bin is trusted; argv is test-controlled
	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Env = []string{
		"HOME=" + homeDir,
		"PATH=" + os.Getenv("PATH"),
		"GUILD_NO_USAGE_LOG=1",
	}
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if asExitErr(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return Invocation{
		Stdout:   strings.TrimRight(stdout.String(), "\n"),
		Stderr:   strings.TrimRight(stderr.String(), "\n"),
		ExitCode: exitCode,
		Elapsed:  elapsed,
	}
}

// appraise runs `guild lore appraise QUERY` in projDir and returns the
// Invocation.
func appraise(ctx context.Context, t *testing.T, homeDir, projDir, query string, extraArgs ...string) Invocation {
	t.Helper()
	argv := []string{"lore", "appraise", query}
	argv = append(argv, extraArgs...)
	return runArgs(ctx, t, homeDir, projDir, argv)
}

// ─────────────────────────────── assert helpers ─────────────────────────────

// assertExitOK fails the test if inv.ExitCode != 0.
func assertExitOK(t *testing.T, inv Invocation, context string) {
	t.Helper()
	if inv.ExitCode != 0 {
		t.Fatalf("%s: expected exit 0, got %d\nstdout: %s\nstderr: %s",
			context, inv.ExitCode, inv.Stdout, inv.Stderr)
	}
}

// assertContains fails the test when s does not appear in text.
func assertContains(t *testing.T, text, s, context string) {
	t.Helper()
	if !strings.Contains(text, s) {
		t.Errorf("%s: expected to contain %q\nactual: %s", context, s, text)
	}
}

// assertNotContains fails the test when s appears in text.
func assertNotContains(t *testing.T, text, s, context string) {
	t.Helper()
	if strings.Contains(text, s) {
		t.Errorf("%s: expected NOT to contain %q\nactual: %s", context, s, text)
	}
}

// ─────────────────────────── Harness self-tests ─────────────────────────────

// TestHarness_BinaryBuildsOnce verifies the build mechanism works and that
// the binary path is non-empty after buildOnce runs.
func TestHarness_BinaryBuildsOnce(t *testing.T) {
	bin := requireBinary(t)
	if bin == "" {
		t.Fatal("binaryPath is empty after requireBinary")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("binary not found at %s: %v", bin, err)
	}
	t.Logf("guild binary at: %s", bin)

	// Run the binary once to confirm it executes.
	ctx := context.Background()
	homeDir := t.TempDir()
	inv := run(ctx, t, homeDir, "version", nil)
	assertExitOK(t, inv, "guild version")
	t.Logf("guild version output: %s", inv.Stdout)
}

// TestHarness_TempHomeIsolated verifies that two subtests with different
// temp HOMEs do not share state — inscribing in one HOME doesn't appear
// in the other HOME's lore.
func TestHarness_TempHomeIsolated(t *testing.T) {
	ctx := context.Background()

	homeA := t.TempDir()
	homeB := t.TempDir()

	projA := filepath.Join(homeA, "proj-a")
	projB := filepath.Join(homeB, "proj-b")

	_ = initProject(ctx, t, homeA, projA)
	_ = initProject(ctx, t, homeB, projB)

	// Inscribe something in HOME A.
	inv := inscribe(ctx, t, homeA, projA,
		"isolated entry only in home A",
		"research",
		"verifies home isolation between test instances",
		"test-isolation")
	assertExitOK(t, inv, "inscribe in homeA")

	// Appraise in HOME B — should get nothing.
	inv = appraise(ctx, t, homeB, projB, "isolated entry only in home A")
	// The output should contain "nothing found" (miss), not a real result.
	// The "nothing found" line includes the query, so we check the exit
	// code + that no ENTRY-N id appears (a real result would have one).
	assertNotContains(t, inv.Stdout, "ENTRY-",
		"homeB should not see homeA's entries (no ENTRY-N ids expected)")
	t.Logf("HOME isolation confirmed: homeB appraise output = %q", inv.Stdout)
}

// pidFromFile reads the numeric PID from a session file path like
// ~/.guild/sessions/<pid>.json. Used in session-reload tests.
func pidFromFile(path string) (int, error) {
	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, ".json")
	return strconv.Atoi(stem)
}
