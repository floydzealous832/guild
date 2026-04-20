package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mathomhaus/guild/internal/config"
)

// maxFieldLen is the maximum byte length to which free-form TSV fields
// (project, subcommand) are truncated before writing.  Keeps every record
// well under POSIX PIPE_BUF (512 bytes minimum), making O_APPEND writes
// atomic on all supported platforms.
const maxFieldLen = 64

// Record appends one TSV record to ~/.guild/usage.log describing a single
// guild command invocation.
//
// TSV field order (6 columns):
//
//	<RFC3339 UTC timestamp>\t<project>\t<subcommand>\t<exit_code>\t<duration_ms>\t<resp_bytes>\n
//
// resp_bytes is the byte count of the rendered response body the caller
// returned to the client. Pass 0 for surfaces that do not produce a
// structured response body (CLI, error paths).
//
// Privacy invariant: the function signature accepts ONLY the six fields
// that appear in the TSV record. There is no parameter for appraise
// queries, inscribe titles/summaries, journal/brief text, file paths,
// or agent IDs — making it structurally impossible to log user content
// through this function.
//
// Telemetry is opt-in: logging is suppressed unless [telemetry] usage_log = true
// is set in config.  GUILD_NO_USAGE_LOG=1 also forces logging off.
// cfg may be nil (treated as disabled to be safe).
//
// Best-effort: if writing fails, Record logs a single ⚠️ warning to stderr
// and returns nil so that the caller's operation is never aborted by a
// telemetry failure.
func Record(ctx context.Context, cfg *config.Config, project, subcommand string, exitCode int, duration time.Duration, respBytes uint) error {
	if isDisabled(cfg) {
		return nil
	}

	// Respect context cancellation before performing I/O.
	if err := ctx.Err(); err != nil {
		return nil //nolint:nilerr // best-effort: cancelled ctx → skip silently
	}

	logPath, err := UsageLogPath()
	if err != nil {
		warnTelemetry("resolve usage log path", err)
		return nil
	}

	dir, _ := guildDir()
	if err := ensureDir(dir); err != nil {
		warnTelemetry("create ~/.guild dir", err)
		return nil
	}

	line := formatUsageLine(project, subcommand, exitCode, duration, respBytes)

	if err := appendLine(logPath, line); err != nil {
		warnTelemetry("write usage.log", err)
		return nil
	}

	slog.DebugContext(ctx, "telemetry: usage recorded",
		"project", project, "subcommand", subcommand,
		"exit_code", exitCode, "duration_ms", duration.Milliseconds(), "resp_bytes", respBytes)

	return nil
}

// formatUsageLine builds the TSV line for one usage record.
// Exported for tests; not part of the public API surface users depend on.
func formatUsageLine(project, subcommand string, exitCode int, duration time.Duration, respBytes uint) string {
	return fmt.Sprintf("%s\t%s\t%s\t%d\t%d\t%d\n",
		time.Now().UTC().Format(time.RFC3339),
		truncate(project, maxFieldLen),
		truncate(subcommand, maxFieldLen),
		exitCode,
		duration.Milliseconds(),
		respBytes,
	)
}

// isDisabled returns true if telemetry logging is off.  Logging is off by
// default (opt-in policy); cfg.NoUsageLog is the merged runtime bit set by
// config.Load whenever Telemetry.UsageLog is false or GUILD_NO_USAGE_LOG=1.
// cfg == nil is treated as disabled (fail-safe).
func isDisabled(cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	return cfg.NoUsageLog
}

// appendLine opens path for append and writes line atomically.
// O_APPEND on POSIX guarantees that writes <= PIPE_BUF are atomic; our lines
// are always well under 512 bytes.
//
// Rotation (QUEST-22): before the write we peek at the file size and
// roll over to path.1 (…path.5) when the active log reaches
// rotationThreshold. The single call site covers both usage.log and
// misses.log since both route through this helper.
func appendLine(path, line string) error {
	rotateIfNeeded(path)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	_, writeErr := fmt.Fprint(f, line)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write %s: %w", path, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", path, closeErr)
	}
	return nil
}

// truncate returns s truncated to at most n bytes (cutting on byte boundaries,
// not rune boundaries, which is fine since subcommand/project names are ASCII).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// warnTelemetry prints a one-line ⚠️ warning to stderr.  Telemetry failures
// must never surface as errors to the user's command — only this warning.
func warnTelemetry(op string, err error) {
	fmt.Fprintf(os.Stderr, "⚠️  guild telemetry: %s: %v\n", op, err)
}
