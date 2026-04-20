package telemetry

import (
	"fmt"
	"os"
)

// rotationThreshold is the size at which the telemetry log files roll
// over — 10 MiB. Chosen to keep disk footprint bounded on machines
// where guild is used heavily (thousands of invocations a week) while
// still retaining weeks-to-months of history across the 5 kept
// archives. See QUEST-22.
const rotationThreshold int64 = 10 * 1024 * 1024

// rotationMaxArchives is the count of numbered archive files kept on
// disk. With threshold 10 MiB and 5 archives, total worst-case
// telemetry disk cost is (1 active + 5 archives) × 10 MiB = 60 MiB per
// log (usage.log + misses.log → 120 MiB ceiling). Older archives are
// unlinked on rotation.
const rotationMaxArchives = 5

// rotateIfNeeded rotates path when its size is at or above
// rotationThreshold, preserving the last rotationMaxArchives
// generations as path.1 … path.N. Safe to call on every write: when
// the file is small or missing it returns nil immediately.
//
// Rotation strategy (classic "rolling" scheme):
//
//  1. path.5 is removed if present (oldest dropped).
//  2. For i from 4 down to 1, path.i is renamed to path.(i+1).
//  3. path is renamed to path.1.
//  4. Caller's next appendLine opens a fresh path in O_CREATE mode.
//
// Failures inside the rename chain are logged as warnings and swallowed
// — telemetry must never abort the caller's command. In the worst case
// the file grows past threshold until the next successful rotation.
func rotateIfNeeded(path string) {
	info, err := os.Stat(path)
	if err != nil {
		// Missing file is the common case on first write — nothing to rotate.
		// Any other stat error means we can't see the size, so we bail
		// silently: better to skip rotation than fail the log write.
		return
	}
	if info.Size() < rotationThreshold {
		return
	}
	performRotation(path)
}

// performRotation executes the rename chain. Separated from
// rotateIfNeeded so tests can exercise it directly with a small
// threshold without racing on size calculations.
func performRotation(path string) {
	// Step 1: drop the oldest archive if present. os.Remove on a
	// nonexistent file returns ENOENT — treat as no-op.
	oldest := archivePath(path, rotationMaxArchives)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		warnTelemetry(fmt.Sprintf("rotate: remove %s", oldest), err)
		// Continue anyway — a stale oldest archive is preferable to
		// aborting the rotation, which would let the active log grow
		// without bound.
	}

	// Step 2: shift each remaining archive one slot higher.
	// Walk from N-1 down to 1 so we never overwrite a file that
	// hasn't been shifted yet.
	for i := rotationMaxArchives - 1; i >= 1; i-- {
		src := archivePath(path, i)
		dst := archivePath(path, i+1)
		if _, err := os.Stat(src); err != nil {
			continue // archive slot empty; skip
		}
		if err := os.Rename(src, dst); err != nil {
			warnTelemetry(fmt.Sprintf("rotate: %s → %s", src, dst), err)
		}
	}

	// Step 3: retire the active log to slot 1.
	if err := os.Rename(path, archivePath(path, 1)); err != nil {
		warnTelemetry(fmt.Sprintf("rotate: %s → %s", path, archivePath(path, 1)), err)
	}
}

// archivePath returns the Nth archive name, e.g. archivePath("u.log", 3)
// → "u.log.3". Slot 0 is reserved for the active file so callers
// should only ever pass n >= 1.
func archivePath(path string, n int) string {
	return fmt.Sprintf("%s.%d", path, n)
}
