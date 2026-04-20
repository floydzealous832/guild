package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// seedSessionFile writes a placeholder session file for the given PID
// under m's BaseDir without going through Save (which would overwrite
// m.PID's file). Used by cleanup tests to populate the directory with
// fake prior-process files.
func seedSessionFile(t *testing.T, m Manager, pid int, content string) string {
	t.Helper()
	dir := filepath.Join(m.BaseDir, "sessions")
	if err := os.MkdirAll(dir, sessionDirMode); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	p := filepath.Join(dir, strconv.Itoa(pid)+".json")
	if err := os.WriteFile(p, []byte(content), sessionFileMode); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	return p
}

// TestCleanupStale_RemovesOnlyDeadPIDs seeds a mix of dead and alive
// PIDs, injects a deterministic aliveness probe, and verifies cleanup
// nukes only the dead ones.
//
// We don't rely on real process scanning (flaky across CI hosts);
// we inject the exact answers the syscall.Kill probe would return.
func TestCleanupStale_RemovesOnlyDeadPIDs(t *testing.T) {
	m := Manager{BaseDir: t.TempDir(), PID: os.Getpid()}

	// Write our own file so cleanup has an explicit "don't touch
	// self" target to skip.
	ownPath := seedSessionFile(t, m, m.PID, `{"active_project":"self"}`)

	alive := seedSessionFile(t, m, 11111, `{"active_project":"a"}`)
	dead1 := seedSessionFile(t, m, 22222, `{"active_project":"b"}`)
	dead2 := seedSessionFile(t, m, 33333, `{"active_project":"c"}`)
	unrelated := filepath.Join(m.BaseDir, "sessions", "README.md")
	if err := os.WriteFile(unrelated, []byte("hi"), sessionFileMode); err != nil {
		t.Fatalf("write unrelated: %v", err)
	}
	// A .json.tmp sibling left behind by a crashed write — cleanup
	// must leave it alone (not its contract; it's the writer's).
	strayTmp := filepath.Join(m.BaseDir, "sessions", "44444.json.tmp")
	if err := os.WriteFile(strayTmp, []byte(`{partial`), sessionFileMode); err != nil {
		t.Fatalf("write tmp: %v", err)
	}

	// Inject aliveness: 11111 is alive (sentinel), the others are not.
	restore := swapProcessAlive(func(pid int) (bool, error) {
		return pid == 11111, nil
	})
	defer restore()

	if err := m.CleanupStale(context.Background()); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}

	// Self and alive survive; dead PIDs removed; non-json ignored.
	mustExist(t, ownPath)
	mustExist(t, alive)
	mustGone(t, dead1)
	mustGone(t, dead2)
	mustExist(t, unrelated)
	mustExist(t, strayTmp)
}

// TestCleanupStale_MissingDirIsNoOp ensures the common cold-start case
// (no ~/.guild/sessions/ yet) never errors.
func TestCleanupStale_MissingDirIsNoOp(t *testing.T) {
	m := Manager{BaseDir: t.TempDir(), PID: os.Getpid()}
	if err := m.CleanupStale(context.Background()); err != nil {
		t.Fatalf("CleanupStale on missing dir: %v", err)
	}
}

// TestCleanupStale_NonPIDFileIgnored verifies a file whose name isn't
// "<int>.json" is left alone. Protects against cleanup nuking a user's
// README if they point a text editor at the sessions directory.
func TestCleanupStale_NonPIDFileIgnored(t *testing.T) {
	m := Manager{BaseDir: t.TempDir(), PID: os.Getpid()}
	dir := filepath.Join(m.BaseDir, "sessions")
	if err := os.MkdirAll(dir, sessionDirMode); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	weird := filepath.Join(dir, "not-a-pid.json")
	if err := os.WriteFile(weird, []byte("{}"), sessionFileMode); err != nil {
		t.Fatalf("seed: %v", err)
	}

	restore := swapProcessAlive(func(int) (bool, error) { return false, nil })
	defer restore()

	if err := m.CleanupStale(context.Background()); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}
	mustExist(t, weird)
}

// TestCleanupStale_ProbeErrorRecordedButLoopContinues — a flaky probe
// on one PID must not prevent cleanup of the next PID in the scan.
func TestCleanupStale_ProbeErrorRecordedButLoopContinues(t *testing.T) {
	m := Manager{BaseDir: t.TempDir(), PID: os.Getpid()}

	seedSessionFile(t, m, 77777, `{}`)
	nextDead := seedSessionFile(t, m, 88888, `{}`)

	var calls int
	restore := swapProcessAlive(func(pid int) (bool, error) {
		calls++
		if pid == 77777 {
			return false, errors.New("simulated flake")
		}
		return false, nil // 88888 is dead → should be removed
	})
	defer restore()

	err := m.CleanupStale(context.Background())
	if err == nil {
		t.Fatalf("want surfaced probe error")
	}
	if calls < 2 {
		t.Fatalf("loop short-circuited after first probe error (calls=%d)", calls)
	}
	mustGone(t, nextDead)
}

// TestCleanupStale_DoesNotRemoveOwnFile is a belt-and-braces check
// for the "don't delete my own state" rule. We seed our PID's file
// and verify it's preserved even if the probe claims we're "dead"
// (defense in depth against a clock-skewed probe).
func TestCleanupStale_DoesNotRemoveOwnFile(t *testing.T) {
	m := Manager{BaseDir: t.TempDir(), PID: 98765}
	own := seedSessionFile(t, m, m.PID, `{"active_project":"self"}`)

	restore := swapProcessAlive(func(int) (bool, error) { return false, nil })
	defer restore()

	if err := m.CleanupStale(context.Background()); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}
	mustExist(t, own)
}

// TestProcessAlive_Self asserts the real probe reports the current
// process as alive. Exercises the syscall.Kill path without any
// injection so we know the plumbing works on this OS.
func TestProcessAlive_Self(t *testing.T) {
	alive, err := realProcessAlive(os.Getpid())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !alive {
		t.Fatalf("self should be alive")
	}
}

// TestProcessAlive_InvalidPID ensures non-positive PIDs don't panic
// and return (false, nil).
func TestProcessAlive_InvalidPID(t *testing.T) {
	alive, err := realProcessAlive(0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if alive {
		t.Fatalf("pid 0 should be !alive")
	}
}

// TestCleanupStale_IntegrationWithMassOfStalePIDs is the volume check
// the spec calls out: create N files with random-but-likely-dead PIDs
// (os.Getpid()+100000), run cleanup, and verify only the live PID's
// file survives.
func TestCleanupStale_Volume(t *testing.T) {
	m := Manager{BaseDir: t.TempDir(), PID: os.Getpid()}

	// Write our own file.
	own := seedSessionFile(t, m, m.PID, "{}")

	// Seed N fake stale files at PIDs that are almost certainly dead
	// (very high numbers — higher than the typical PID max).
	const N = 50
	stale := make([]string, N)
	base := os.Getpid() + 100_000
	for i := 0; i < N; i++ {
		stale[i] = seedSessionFile(t, m, base+i, fmt.Sprintf("{}"))
	}

	// Real probe, not a fake — makes the test prove the actual
	// syscall.Kill semantics work.
	if err := m.CleanupStale(context.Background()); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}

	mustExist(t, own)
	for _, f := range stale {
		mustGone(t, f)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist, got %v", path, err)
	}
}

func mustGone(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		t.Fatalf("expected %s to be removed", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected stat err: %v", err)
	}
}
