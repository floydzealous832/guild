// Package session owns the per-PID JSON session-state file the guild MCP
// server uses to remember the active project across restarts, and the
// MCP-side project resolution order.
//
// Design decisions:
//
//   - One file per MCP server PID at ~/.guild/sessions/<pid>.json. NEVER
//     a single global ~/.guild/active_project — that races across
//     concurrent Claude Code sessions and the last writer wins for both.
//   - Every mutation goes through an atomic rename: we write
//     ~/.guild/sessions/<pid>.json.tmp, then os.Rename to the final
//     name. A crashed write can leave a stray .tmp but never a partial
//     .json file that a concurrent reader could pick up.
//   - Every access that could cross a restart boundary re-reads from
//     disk. Don't cache state in a long-lived struct; the subprocess
//     can be SIGKILL'd at any time.
//   - Stale cleanup (cleanup.go) runs exactly ONCE per server start —
//     cheap, never per-tool-call.
//
// This package is intentionally free of DB dependencies — the session
// file is just a JSON blob keyed by PID and a small set of well-known
// keys (currently only `active_project`). Project-registry CRUD lives
// in `internal/project`.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

// sessionFileMode is the mode bits for ~/.guild/sessions/<pid>.json.
// The file carries nothing secret but is still per-user state; 0600
// keeps it private on shared machines without being unnecessarily
// restrictive.
const sessionFileMode = 0o600

// sessionDirMode matches the rest of ~/.guild (user-owned; group/world
// blocked).
const sessionDirMode = 0o700

// tempSuffix is the suffix of the temp file used for atomic rename.
// We write <pid>.json.<counter>.tmp, then os.Rename to <pid>.json.
// Cleanup's suffix check (".json" only) leaves all .tmp variants
// alone. The counter is an in-process atomic to make each call
// within a PID use a distinct temp path — prevents two goroutines on
// the same PID from racing on the same tmp file and corrupting each
// other's writes.
const tempSuffix = ".tmp"

// tempCounter distinguishes concurrent temp-file names within one
// process. Monotonically increasing across all managers; a rollover
// is not a concern (uint64 never will during a real program lifetime).
var tempCounter atomic.Uint64

// processNonce is a random token minted once per process lifetime and
// embedded in every session file this process writes. Load uses it to
// detect stale files left by a dead prior process that the OS reused
// the same PID for.
var processNonce = mustMintNonce()

func mustMintNonce() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is extremely rare and unrecoverable; panic
		// rather than silently producing a weak or empty nonce.
		panic("session: mint process nonce: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// swapProcessNonce replaces processNonce with n and returns a restore
// closure. Test-only; package-private.
func swapProcessNonce(n string) (restore func()) {
	prev := processNonce
	processNonce = n
	return func() { processNonce = prev }
}

// State is the on-disk session blob. JSON keys are stable; adding new
// keys is append-only so older binaries can still read newer files.
//
// `active_project` is the only key defined today. Keep the struct
// tolerant of additional keys by round-tripping unknown JSON via
// json.RawMessage if that ever becomes necessary.
type State struct {
	ActiveProject string `json:"active_project,omitempty"`
	// ProcessNonce ties this file to a specific process invocation so
	// that a new process that inherits the same PID via OS reuse can
	// detect the mismatch and discard the stale state.
	ProcessNonce string `json:"process_nonce,omitempty"`
}

// Manager owns the filesystem and process-id seams Load/Save consume.
// Production code uses the package-level Load/Save/SetActiveProject
// helpers, which default to the real home directory + real PID.
//
// Tests construct their own Manager literal with temp-dir + arbitrary
// PID so they can simulate two concurrent MCP servers from a single
// test process.
type Manager struct {
	// BaseDir is the ~/.guild/ equivalent — i.e. the parent of
	// sessions/. Defaults via homeSessionsBase() to $HOME/.guild.
	BaseDir string
	// PID is the MCP server's own PID. Defaults to os.Getpid().
	PID int
}

// DefaultManager is the process-wide Manager resolved lazily via
// defaultManager(). We resolve $HOME at each call rather than capturing
// it at import time so tests can override HOME via t.Setenv without a
// package-init race.
func defaultManager() (Manager, error) {
	base, err := homeSessionsBase()
	if err != nil {
		return Manager{}, err
	}
	return Manager{BaseDir: base, PID: os.Getpid()}, nil
}

// homeSessionsBase returns the equivalent of $HOME/.guild. Split out
// so tests can exercise the HOME-unresolvable branch.
func homeSessionsBase() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("session: resolve home: %w", err)
	}
	return filepath.Join(home, ".guild"), nil
}

// Path returns the full path to the default manager's session file:
// ~/.guild/sessions/<pid>.json. Exposed so callers (and tests) can
// assert on the resolved location without reimplementing the layout.
//
// Returns "" and an error only if the home directory can't be
// resolved; every other path is always-constructible once the home is
// known.
func Path() (string, error) {
	m, err := defaultManager()
	if err != nil {
		return "", err
	}
	return m.Path(), nil
}

// Path returns the <pid>.json location for this Manager. Pure
// string-concat; no IO.
func (m Manager) Path() string {
	return filepath.Join(m.BaseDir, "sessions", strconv.Itoa(m.PID)+".json")
}

// Load reads and decodes the session file for the default manager. A
// missing file returns a zero-value *State with nil error — "no active
// project yet" is the common case at server startup, not an error.
//
// Corrupt-or-partial JSON returns an error so the caller can decide
// whether to wipe the file and retry or bail. In practice the atomic
// rename in Save prevents partial files; the main corruption path is
// user tampering or a filesystem fault.
func Load(ctx context.Context) (*State, error) {
	m, err := defaultManager()
	if err != nil {
		return nil, err
	}
	return m.Load(ctx)
}

// Load is the method form used by tests. Takes ctx for symmetry with
// the rest of the API even though file IO on a local JSON blob can't
// meaningfully observe cancellation.
func (m Manager) Load(_ context.Context) (*State, error) {
	path := m.Path()
	// G304: path is built from a trusted BaseDir + os.Getpid() int;
	// no user-controlled components feed into it at runtime.
	data, err := os.ReadFile(path) //nolint:gosec // trusted path; see note above
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{}, nil
		}
		return nil, fmt.Errorf("session: read %s: %w", path, err)
	}
	if len(data) == 0 {
		// Treat an empty file the same as a missing one. Avoids a
		// confusing "unexpected end of JSON input" when the process
		// was killed between create and first write.
		return &State{}, nil
	}
	s := &State{}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("session: parse %s: %w", path, err)
	}
	// If the file carries a nonce written by a different process
	// invocation, a dead process once owned this PID and the OS has
	// since reused it for us. Discard the stale state rather than
	// inheriting its active_project.
	if s.ProcessNonce != "" && s.ProcessNonce != processNonce {
		return &State{}, nil
	}
	return s, nil
}

// Save atomically writes s to the default manager's session file.
// Creates ~/.guild/sessions/ on demand. Atomicity: write to
// <pid>.json.tmp, then os.Rename to <pid>.json. A crash between write
// and rename leaves a .tmp file which stale cleanup ignores (wrong
// suffix) but does NOT produce a partially-written .json.
func Save(ctx context.Context, s *State) error {
	m, err := defaultManager()
	if err != nil {
		return err
	}
	return m.Save(ctx, s)
}

// Save is the method form used by tests.
func (m Manager) Save(_ context.Context, s *State) error {
	if s == nil {
		return fmt.Errorf("session: save: nil state")
	}

	final := m.Path()
	dir := filepath.Dir(final)
	if err := os.MkdirAll(dir, sessionDirMode); err != nil {
		return fmt.Errorf("session: mkdir %s: %w", dir, err)
	}

	// Stamp this write with the current process nonce so a future
	// process that reuses this PID can detect the file is stale.
	s.ProcessNonce = processNonce

	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("session: marshal: %w", err)
	}

	// Write to a unique tmp sibling first. Each call gets its own
	// counter-suffixed tmp so two goroutines on the same PID don't
	// race on the same filename. We then rename over the final path
	// — rename is atomic on POSIX and good-enough on Windows NTFS for
	// our purposes (see platform notes in cleanup.go).
	tmp := fmt.Sprintf("%s.%d%s", final, tempCounter.Add(1), tempSuffix)
	// G304: tmp is derived from trusted BaseDir + pid int + in-process
	// counter; no user-controlled input feeds into the path.
	f, err := os.OpenFile(tmp, //nolint:gosec // trusted path; see note above
		os.O_WRONLY|os.O_CREATE|os.O_EXCL, sessionFileMode)
	if err != nil {
		return fmt.Errorf("session: create %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("session: write %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("session: close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("session: rename %s -> %s: %w", tmp, final, err)
	}
	return nil
}

// SetActiveProject reads the current state, updates active_project,
// and writes the result back atomically. Convenience wrapper around
// Load + Save for the common case.
//
// Uses the default manager. Tests that need per-PID simulation should
// call Manager.SetActiveProject on their own Manager literal.
func SetActiveProject(ctx context.Context, name string) error {
	m, err := defaultManager()
	if err != nil {
		return err
	}
	return m.SetActiveProject(ctx, name)
}

// SetActiveProject is the method form used by tests.
func (m Manager) SetActiveProject(ctx context.Context, name string) error {
	if name = strings.TrimSpace(name); name == "" {
		return fmt.Errorf("session: set active project: empty name")
	}
	s, err := m.Load(ctx)
	if err != nil {
		return err
	}
	s.ActiveProject = name
	return m.Save(ctx, s)
}

// ResolveForMCP implements the MCP-server project-resolution order:
//
//  1. explicit `arg` (non-empty) → use it
//  2. else: read ~/.guild/sessions/<own_pid>.json active_project
//  3. else: the `env` value (typically GUILD_PROJECT)
//  4. else: friendly error pointing the agent at guild_session_start
//
// `env` is passed in rather than read internally so the caller can
// share its environment-resolution policy with `internal/config` and
// so the test fixture doesn't have to os.Setenv. Pass "" to skip
// step 3.
func ResolveForMCP(ctx context.Context, arg, env string) (string, error) {
	m, err := defaultManager()
	if err != nil {
		return "", err
	}
	return m.ResolveForMCP(ctx, arg, env)
}

// ResolveForMCP is the method form used by tests.
func (m Manager) ResolveForMCP(ctx context.Context, arg, env string) (string, error) {
	// Step 1: explicit arg wins.
	if name := strings.TrimSpace(arg); name != "" {
		return name, nil
	}

	// Step 2: per-PID session file. A missing file is NOT an error
	// here (Load returns zero-value State); we just fall through.
	s, err := m.Load(ctx)
	if err != nil {
		// Parse errors etc. surface verbatim — the agent sees what
		// went wrong and can remove the bad file.
		return "", err
	}
	if name := strings.TrimSpace(s.ActiveProject); name != "" {
		return name, nil
	}

	// Step 3: env-supplied default (typically $GUILD_PROJECT).
	if name := strings.TrimSpace(env); name != "" {
		return name, nil
	}

	// Step 4: friendly error guiding the agent to the recovery path.
	return "", errors.New(
		"[error] no active project set — " +
			"call guild_session_start(project='<name>') first to bootstrap the session, " +
			"or pass project='<name>' explicitly to this tool. " +
			"(The MCP server may have restarted — common after file edits or idle. " +
			"Just re-bootstrap and retry your last call; nothing is lost.)")
}
