package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CleanupStale scans ~/.guild/sessions/ and deletes every <pid>.json
// whose PID is no longer running. Safe to call once per server
// startup; startup cleanup beats graceful-shutdown cleanup because the
// host can SIGKILL without warning.
//
// Idempotent and cheap: an empty or missing sessions directory is a
// no-op. Never deletes a file for a PID that's alive — the probe
// `syscall.Kill(pid, 0)` returns nil (process exists) or errno
// ESRCH/ENOENT (process gone). EPERM is treated as alive (the process
// exists but belongs to another user, probably a different guild
// install; leaving the file alone is safe).
//
// We NEVER touch our own <pid>.json file — even though the common case
// is that it doesn't exist yet at startup, a test could prime it.
func CleanupStale(ctx context.Context) error {
	m, err := defaultManager()
	if err != nil {
		return err
	}
	return m.CleanupStale(ctx)
}

// CleanupStale is the method form used by tests.
//
// Accepts ctx so future cross-platform implementations that shell out
// for process-probing (e.g. Windows' OpenProcess) can honor
// cancellation. On Unix the syscall.Kill probe is purely local and
// can't meaningfully observe ctx, but we accept it anyway to keep the
// signature stable.
func (m Manager) CleanupStale(_ context.Context) error {
	dir := filepath.Join(m.BaseDir, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("session: cleanup: read %s: %w", dir, err)
	}

	var firstErr error
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// We only own .json files in this directory. A .json.tmp file
		// is an in-flight atomic write — leave it alone; the owning
		// process will either rename it shortly or die and a future
		// cleanup will see it as leftover (we deliberately do NOT
		// delete .tmp files here because we can't tell whether the
		// owning process is still mid-write).
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		stem := strings.TrimSuffix(name, ".json")
		pid, err := strconv.Atoi(stem)
		if err != nil || pid <= 0 {
			// Unrelated file — skip rather than error. Preserves
			// admin-placed README.md / editor swap files under
			// sessions/ if someone ever points a text editor at the
			// directory.
			continue
		}

		// Don't remove our own file — we may have just written it.
		if pid == m.PID {
			continue
		}

		alive, err := processAlive(pid)
		if err != nil {
			// Record but keep going; one flaky probe shouldn't abort
			// cleanup of the rest.
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if alive {
			continue
		}

		target := filepath.Join(dir, name)
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			if firstErr == nil {
				firstErr = fmt.Errorf("session: cleanup: remove %s: %w", target, err)
			}
		}
	}
	return firstErr
}

// processAlive reports whether a process with the given PID exists.
//
// Unix: syscall.Kill(pid, 0) returns nil (exists), ESRCH/ENOENT (gone),
// or EPERM (exists but owned by another user). EPERM is treated as
// alive — we don't have authority to probe it precisely, and leaving
// an unknown-state file alone is always safe.
//
// Windows: syscall.Kill is not implemented. See the build-tagged
// cleanup_windows.go file for the OpenProcess-based probe; this file
// covers darwin, linux, and BSDs (the hosts Claude Code runs on
// today).
//
// This function is split out so that tests can swap in a fake probe
// via the package-level processAliveFn variable below.
func processAlive(pid int) (bool, error) { return processAliveFn(pid) }

// processAliveFn is the indirection test code overrides. Swap it via
// swapProcessAlive() in a test and restore the original in a
// t.Cleanup hook.
var processAliveFn = realProcessAlive

// swapProcessAlive replaces processAliveFn with fn and returns a
// restore closure. Test-only; package-private.
func swapProcessAlive(fn func(int) (bool, error)) (restore func()) {
	prev := processAliveFn
	processAliveFn = fn
	return func() { processAliveFn = prev }
}

// realProcessAlive is provided per-platform:
//
//   - Unix (darwin, linux, BSDs): cleanup_unix.go implements it via
//     syscall.Kill(pid, 0) + errno ESRCH/EPERM handling.
//   - Windows: cleanup_windows.go implements it via os.FindProcess +
//     best-effort probe; platform notes journaled at QUEST-4 time.
