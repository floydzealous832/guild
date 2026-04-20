package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newManager returns a Manager scoped to a fresh t.TempDir so tests
// never step on the real ~/.guild. PID is pinned so race tests can
// simulate distinct MCP server processes deterministically.
func newManager(t *testing.T, pid int) Manager {
	t.Helper()
	return Manager{BaseDir: t.TempDir(), PID: pid}
}

// TestManagerPath_SchemaMatchesSpec locks the filename scheme:
// per-PID naming — <pid>.json under sessions/ — NOT a single
// global file.
func TestManagerPath_SchemaMatchesSpec(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "home", "x", ".guild")
	m := Manager{BaseDir: base, PID: 12345}
	got := m.Path()
	want := filepath.Join(base, "sessions", "12345.json")
	if got != want {
		t.Fatalf("path: want %q, got %q", want, got)
	}
}

func TestLoad_MissingFileReturnsZeroValue(t *testing.T) {
	m := newManager(t, 4242)
	s, err := m.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s == nil || s.ActiveProject != "" {
		t.Fatalf("want empty state, got %+v", s)
	}
}

func TestSave_CreatesFileAtomically(t *testing.T) {
	m := newManager(t, 4242)
	ctx := context.Background()

	if err := m.Save(ctx, &State{ActiveProject: "guild"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// File exists at the expected path.
	info, err := os.Stat(m.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("file is empty")
	}
	// No .tmp siblings left behind. We walk the sessions/ dir rather
	// than asserting a specific tmp name because Save's tmp name
	// includes a per-call counter.
	entries, err := os.ReadDir(filepath.Dir(m.Path()))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), tempSuffix) {
			t.Fatalf("tmp file left behind: %s", e.Name())
		}
	}
}

func TestSaveLoad_RoundTripsActiveProject(t *testing.T) {
	m := newManager(t, 4242)
	ctx := context.Background()

	if err := m.SetActiveProject(ctx, "guild"); err != nil {
		t.Fatalf("SetActiveProject: %v", err)
	}

	s, err := m.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.ActiveProject != "guild" {
		t.Fatalf("want guild, got %q", s.ActiveProject)
	}
}

// TestSameProcessRoundTrip verifies that within one process lifetime,
// a Save followed by Load on the same PID round-trips the active_project.
// This is the normal "tool call N saves, tool call N+1 loads" path.
func TestSameProcessRoundTrip(t *testing.T) {
	ctx := context.Background()
	m := Manager{BaseDir: t.TempDir(), PID: 1001}
	if err := m.SetActiveProject(ctx, "guild"); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := m.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.ActiveProject != "guild" {
		t.Fatalf("want guild, got %q", s.ActiveProject)
	}
}

// TestPIDReuseDoesNotResurrectStaleProject is the primary regression
// test for the OS PID-reuse bug. It simulates:
//
//  1. Process A (nonce "old") writes a session file for PID 1001.
//  2. Process A dies.
//  3. OS assigns PID 1001 to Process B (nonce "new").
//  4. Process B reads the file and MUST NOT inherit Process A's project.
func TestPIDReuseDoesNotResurrectStaleProject(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()

	// Process A writes state with nonce "old".
	restoreA := swapProcessNonce("old-process-nonce")
	a := Manager{BaseDir: base, PID: 1001}
	if err := a.SetActiveProject(ctx, "stale-project"); err != nil {
		t.Fatalf("write: %v", err)
	}
	restoreA()

	// Process A dies. OS reuses PID 1001 for Process B (nonce "new").
	restoreB := swapProcessNonce("new-process-nonce")
	defer restoreB()

	b := Manager{BaseDir: base, PID: 1001}
	s, err := b.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Process B must see a blank state, not the stale active_project.
	if s.ActiveProject != "" {
		t.Fatalf("stale active_project resurrected: %q", s.ActiveProject)
	}
}

// TestPartialWriteNeverBecomesVisible simulates a crash mid-write:
// a .json.tmp file exists but the final .json does NOT. Load must
// return zero-value state (file missing), never a partial read.
//
// This is the core contract of the atomic-rename approach: the tmp
// file is invisible to readers until Save's os.Rename runs.
func TestPartialWriteNeverBecomesVisible(t *testing.T) {
	m := newManager(t, 7777)
	ctx := context.Background()

	// Set up a .tmp as if a previous process crashed mid-write.
	if err := os.MkdirAll(filepath.Dir(m.Path()), sessionDirMode); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tmp := m.Path() + tempSuffix
	if err := os.WriteFile(tmp, []byte("{partial"), sessionFileMode); err != nil {
		t.Fatalf("seed tmp: %v", err)
	}

	// The final file is absent: Load returns zero-value, no error.
	s, err := m.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.ActiveProject != "" {
		t.Fatalf("partial write leaked into Load: %+v", s)
	}

	// A subsequent Save must succeed and clobber/rename cleanly;
	// the old .tmp may still be present on disk (we don't crash-recover
	// stray .tmp files actively) but must not corrupt the new write.
	if err := m.SetActiveProject(ctx, "guild"); err != nil {
		t.Fatalf("Save after partial: %v", err)
	}
	got, err := m.Load(ctx)
	if err != nil {
		t.Fatalf("Load#2: %v", err)
	}
	if got.ActiveProject != "guild" {
		t.Fatalf("want guild, got %+v", got)
	}
}

// TestLoad_CorruptJSONErrors verifies a genuinely corrupt final file
// (not a .tmp; e.g. user tampered) surfaces as an error rather than
// being silently papered over as empty state.
func TestLoad_CorruptJSONErrors(t *testing.T) {
	m := newManager(t, 8888)
	if err := os.MkdirAll(filepath.Dir(m.Path()), sessionDirMode); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(m.Path(), []byte("{not-json"), sessionFileMode); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := m.Load(context.Background())
	if err == nil {
		t.Fatalf("want parse error")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Fatalf("want parse error, got %v", err)
	}
}

// TestLoad_EmptyFileIsZeroValue locks the contract: an empty file
// (the crash-between-create-and-write window) is treated as no-state
// rather than a parse error.
func TestLoad_EmptyFileIsZeroValue(t *testing.T) {
	m := newManager(t, 9999)
	if err := os.MkdirAll(filepath.Dir(m.Path()), sessionDirMode); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(m.Path(), []byte{}, sessionFileMode); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s, err := m.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.ActiveProject != "" {
		t.Fatalf("want empty, got %+v", s)
	}
}

// TestTwoPIDRace is the critical per-PID regression test. Two goroutines
// each own a distinct "simulated PID" and race SetActiveProject
// concurrently many times. The invariant: neither file ever sees the
// other's active_project, and both files exist independently on disk
// at the end.
//
// This is the clobber reproducer: if we accidentally collapsed to a
// single global file, the final state of both PIDs would agree — and
// that agreement itself would be the bug.
func TestTwoPIDRace(t *testing.T) {
	base := t.TempDir()
	a := Manager{BaseDir: base, PID: 10001}
	b := Manager{BaseDir: base, PID: 20002}

	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine A continuously writes "guild".
	go func() {
		defer wg.Done()
		ctx := context.Background()
		for i := 0; i < iterations; i++ {
			if err := a.SetActiveProject(ctx, "guild"); err != nil {
				t.Errorf("A set: %v", err)
				return
			}
		}
	}()

	// Goroutine B continuously writes "other".
	go func() {
		defer wg.Done()
		ctx := context.Background()
		for i := 0; i < iterations; i++ {
			if err := b.SetActiveProject(ctx, "other"); err != nil {
				t.Errorf("B set: %v", err)
				return
			}
		}
	}()

	wg.Wait()

	// Both files exist independently.
	gotA, err := a.Load(context.Background())
	if err != nil {
		t.Fatalf("A load: %v", err)
	}
	gotB, err := b.Load(context.Background())
	if err != nil {
		t.Fatalf("B load: %v", err)
	}
	if gotA.ActiveProject != "guild" {
		t.Fatalf("A clobbered: want guild, got %q", gotA.ActiveProject)
	}
	if gotB.ActiveProject != "other" {
		t.Fatalf("B clobbered: want other, got %q", gotB.ActiveProject)
	}
	// And they must be stored as DISTINCT files on disk.
	if a.Path() == b.Path() {
		t.Fatalf("PIDs collided on same path: %s", a.Path())
	}
	if _, err := os.Stat(a.Path()); err != nil {
		t.Fatalf("A file missing: %v", err)
	}
	if _, err := os.Stat(b.Path()); err != nil {
		t.Fatalf("B file missing: %v", err)
	}
}

// TestConcurrentSaveSamePIDDoesntProduceGarbage covers the rarer case
// where one MCP server has two goroutines writing to its own session
// file. Atomic-rename means the last writer wins and the file is
// always well-formed JSON; no partial content ever leaks.
func TestConcurrentSaveSamePID(t *testing.T) {
	m := newManager(t, 55555)
	const iterations = 100

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		name := fmt.Sprintf("proj-%d", i)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			for j := 0; j < iterations; j++ {
				if err := m.SetActiveProject(ctx, name); err != nil {
					// Two goroutines racing on the same .tmp file via
					// O_EXCL can surface an "already exists" error —
					// that's a legitimate write-collision outcome,
					// NOT a bug. It means the other goroutine is
					// mid-write and this one's attempt is visible as
					// a retryable error, not corrupt state.
					if !strings.Contains(err.Error(), "exists") {
						t.Errorf("unexpected save error: %v", err)
					}
				}
			}
		}()
	}
	wg.Wait()

	// Whatever state won the race must be well-formed JSON and the
	// active_project must be one of the names written.
	s, err := m.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.HasPrefix(s.ActiveProject, "proj-") {
		t.Fatalf("corrupt final state: %+v", s)
	}

	// Double-check by re-unmarshalling the raw bytes: the file must be
	// valid JSON (proves atomicity even under contention).
	raw, err := os.ReadFile(m.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var reparse State
	if err := json.Unmarshal(raw, &reparse); err != nil {
		t.Fatalf("file is corrupt JSON: %v\n%s", err, raw)
	}
}

// TestResolveForMCP_PrecedenceTable walks the MCP resolution order:
// arg → session file → env → error.
func TestResolveForMCP_PrecedenceTable(t *testing.T) {
	type tcase struct {
		name          string
		arg           string
		sessionActive string // seeded into Manager's session file
		env           string
		want          string
		wantErr       bool
		errContains   string
	}
	cases := []tcase{
		{
			name: "arg wins over session and env",
			arg:  "from-arg", sessionActive: "from-session", env: "from-env",
			want: "from-arg",
		},
		{
			name:          "session wins over env when arg empty",
			sessionActive: "from-session", env: "from-env",
			want: "from-session",
		},
		{
			name: "env wins when arg and session empty",
			env:  "from-env", want: "from-env",
		},
		{
			name:        "all empty → friendly MCP error",
			wantErr:     true,
			errContains: "guild_session_start",
		},
		{
			name: "whitespace-only arg counts as empty",
			arg:  "   ", sessionActive: "from-session",
			want: "from-session",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newManager(t, 33333)
			ctx := context.Background()
			if tc.sessionActive != "" {
				if err := m.SetActiveProject(ctx, tc.sessionActive); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}
			got, err := m.ResolveForMCP(ctx, tc.arg, tc.env)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("err %q missing %q", err, tc.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// TestResolveForMCP_ErrorShapeGuidesRecovery asserts the error shape:
// self-describing + recovery action.
func TestResolveForMCP_ErrorShapeGuidesRecovery(t *testing.T) {
	m := newManager(t, 44444)
	_, err := m.ResolveForMCP(context.Background(), "", "")
	if err == nil {
		t.Fatalf("want error")
	}
	msg := err.Error()
	// Self-describing.
	if !strings.Contains(msg, "no active project set") {
		t.Fatalf("message missing self-description: %q", msg)
	}
	// Guides recovery.
	if !strings.Contains(msg, "guild_session_start") {
		t.Fatalf("message missing recovery guidance: %q", msg)
	}
	// Tagged recoverable.
	if !strings.Contains(msg, "[error]") {
		t.Fatalf("message missing [error] recoverability marker: %q", msg)
	}
}

// TestSetActiveProject_RejectsEmpty keeps the Save contract honest:
// explicit empty overwrites look like typos and must surface as
// errors.
func TestSetActiveProject_RejectsEmpty(t *testing.T) {
	m := newManager(t, 22222)
	if err := m.SetActiveProject(context.Background(), "  "); err == nil {
		t.Fatalf("want error on empty name")
	}
}

// TestDefaultManagerHoneorsHOME proves the package-level Load/Save
// helpers route through $HOME. Uses t.Setenv so it's race-safe under
// `go test -race`.
func TestDefaultManagerHonorsHOME(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	ctx := context.Background()
	if err := SetActiveProject(ctx, "guild"); err != nil {
		t.Fatalf("SetActiveProject: %v", err)
	}

	p, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if !strings.HasPrefix(p, dir) {
		t.Fatalf("Path %q not under HOME %q", p, dir)
	}
	s, err := Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.ActiveProject != "guild" {
		t.Fatalf("want guild, got %q", s.ActiveProject)
	}
}
