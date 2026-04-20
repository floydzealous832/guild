package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRotateIfNeeded_UnderThreshold_NoOp guards the common case:
// small files must never be rotated on write.
func TestRotateIfNeeded_UnderThreshold_NoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.log")
	if err := os.WriteFile(path, []byte("small"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	rotateIfNeeded(path)

	// The active file must still be the same one, same content.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after rotate: %v", err)
	}
	if string(got) != "small" {
		t.Errorf("content changed: %q", got)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("archive was created for small file; err=%v", err)
	}
}

// TestRotateIfNeeded_MissingFile_NoOp handles the bootstrap case
// (first write ever → file doesn't exist yet).
func TestRotateIfNeeded_MissingFile_NoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.log")
	// No file created.
	rotateIfNeeded(path)
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("rotation created archive for missing file")
	}
}

// TestPerformRotation_RenamesAndCapsArchives is the core contract:
// after rotation the active slot is empty (no file), path.1 holds
// the previous active content, and the archive count never exceeds
// rotationMaxArchives.
func TestPerformRotation_RenamesAndCapsArchives(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.log")

	// Seed: create active + 5 archives with distinguishable content
	// so we can verify the shift order.
	write(t, path, "active")
	for i := 1; i <= rotationMaxArchives; i++ {
		write(t, archivePath(path, i), "archive-"+itoa(i))
	}

	performRotation(path)

	// Active slot must be gone (caller's OpenFile(O_CREATE) will remake it).
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("active file still exists after rotation; err=%v", err)
	}

	// path.1 must now hold what was active.
	if got := read(t, archivePath(path, 1)); got != "active" {
		t.Errorf("path.1 = %q; want %q", got, "active")
	}

	// path.2 must hold what path.1 used to: "archive-1".
	if got := read(t, archivePath(path, 2)); got != "archive-1" {
		t.Errorf("path.2 = %q; want %q", got, "archive-1")
	}

	// path.5 must hold what path.4 used to: "archive-4".
	if got := read(t, archivePath(path, rotationMaxArchives)); got != "archive-4" {
		t.Errorf("path.%d = %q; want %q",
			rotationMaxArchives, got, "archive-4")
	}

	// The pre-rotation oldest (archive-5) must be unlinked entirely —
	// no archive beyond path.N.
	if _, err := os.Stat(archivePath(path, rotationMaxArchives+1)); !os.IsNotExist(err) {
		t.Errorf("rotation left path.%d behind; err=%v",
			rotationMaxArchives+1, err)
	}

	// Count total archives on disk — must be exactly rotationMaxArchives.
	entries, _ := os.ReadDir(dir)
	archiveCount := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "usage.log.") {
			archiveCount++
		}
	}
	if archiveCount != rotationMaxArchives {
		t.Errorf("on-disk archive count = %d; want %d",
			archiveCount, rotationMaxArchives)
	}
}

// TestAppendLine_TriggersRotationAtThreshold is the integration
// check: the call site (appendLine) calls rotateIfNeeded so writes
// past the threshold produce a rotated archive.
func TestAppendLine_TriggersRotationAtThreshold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.log")

	// Seed the file at exactly the rotation threshold so the next
	// write triggers a roll. Use bytes.Repeat to avoid allocating
	// 10 MiB of user content in the test source.
	if err := os.WriteFile(path, make([]byte, rotationThreshold), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := appendLine(path, "one\n"); err != nil {
		t.Fatalf("appendLine: %v", err)
	}

	// Active file should exist and contain only the new line (the
	// old threshold-sized content was rotated to path.1).
	newContent := read(t, path)
	if newContent != "one\n" {
		t.Errorf("active after rotation = %q; want %q", newContent, "one\n")
	}
	if _, err := os.Stat(archivePath(path, 1)); err != nil {
		t.Errorf("expected path.1 archive, got err=%v", err)
	}
}

// Helpers -------------------------------------------------------------

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// itoa avoids pulling strconv into this file's visible import list
// for a single decimal conversion in test seeding.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
