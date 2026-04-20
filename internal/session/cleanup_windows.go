//go:build windows

package session

import (
	"errors"
	"os"
)

// realProcessAlive (Windows) uses os.FindProcess + a best-effort
// signal-0 equivalent. Platform notes:
//
//   - os.FindProcess on Windows does a real OpenProcess under the
//     hood, so a nil error means the PID is valid at probe time.
//     Callers that want stronger guarantees (e.g. the PID was not
//     recycled) must track a process handle, which we do not.
//   - Process.Signal(syscall.Signal(0)) reports "not supported by
//     windows" even for live PIDs, so we cannot replicate the Unix
//     behavior precisely. Instead we treat a successful FindProcess
//     as "alive" — with the caveat documented below.
//
// Known limitations (journaled at QUEST-4; revisit with stronger
// primitives when a real Windows user appears):
//
//   - PID recycling: a dead PID that has been reused by the OS will
//     look "alive" here, so its stale session file will survive one
//     extra cleanup pass. The atomic-rename contract in Save still
//     keeps writes safe; the worst case is a few bytes of disk waste
//     until the reused process exits.
//   - ERROR_INVALID_PARAMETER / ERROR_ACCESS_DENIED from OpenProcess
//     surfaces via os.FindProcess as a generic error; we bubble it
//     out so cleanup records it (but keeps scanning the other files).
//
// If a real Windows user shows up, upgrade this to a direct
// windows.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION) + GetExitCodeProcess
// STILL_ACTIVE check. That's the canonical "is this PID alive" idiom
// on Windows and avoids the recycling ambiguity.
func realProcessAlive(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		// os.FindProcess never returns an error on Windows except for
		// OS-level failures; treat as "cannot probe" and report.
		return false, err
	}
	// Release the handle so we don't leak it across many probes.
	// (On Unix Process has no handle to release; this is Windows-only.)
	defer func() { _ = proc.Release() }()

	// Best-effort signal probe. We accept false positives (see limits
	// above) in exchange for a probe that doesn't shell out.
	if err := proc.Signal(os.Signal(nil)); err == nil {
		return true, nil
	} else if errors.Is(err, os.ErrProcessDone) {
		return false, nil
	}
	// Other errors are usually "not supported"; fall back to the
	// optimistic "the FindProcess succeeded, assume alive" answer.
	return true, nil
}
