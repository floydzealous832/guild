package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mathomhaus/guild/internal/config"
)

// maxQueryLen is the maximum byte length to which query strings are truncated
// before writing to misses.log.  Keeps records well under POSIX PIPE_BUF.
const maxQueryLen = 256

// RecordMiss appends one TSV record to ~/.guild/misses.log when a lore appraise
// query returns zero results.
//
// TSV field order:
//
//	<RFC3339 UTC timestamp>\t<project>\t<query>\n
//
// Privacy note: the query string IS intentionally recorded here.
// Unlike usage.log, misses.log captures the verbatim search query because that
// is the corpus-improvement signal the misses log exists to provide — it is the
// retrieval system's input, not user-authored content such as an inscribed entry
// title or summary.
//
// Opt-out: honoured identically to Record — both GUILD_NO_USAGE_LOG=1 and
// [telemetry] usage_log = false suppress all writes to misses.log.
//
// Best-effort: write failures emit a ⚠️ warning to stderr and return nil.
func RecordMiss(ctx context.Context, cfg *config.Config, project, query string) error {
	if isDisabled(cfg) {
		return nil
	}

	// Respect context cancellation before performing I/O.
	if err := ctx.Err(); err != nil {
		return nil //nolint:nilerr // best-effort: cancelled ctx → skip silently
	}

	logPath, err := MissesLogPath()
	if err != nil {
		warnTelemetry("resolve misses log path", err)
		return nil
	}

	dir, _ := guildDir()
	if err := ensureDir(dir); err != nil {
		warnTelemetry("create ~/.guild dir", err)
		return nil
	}

	line := formatMissLine(project, query)

	if err := appendLine(logPath, line); err != nil {
		warnTelemetry("write misses.log", err)
		return nil
	}

	slog.DebugContext(ctx, "telemetry: miss recorded",
		"project", project, "query_len", len(query))

	return nil
}

// formatMissLine builds the TSV line for one miss record.
func formatMissLine(project, query string) string {
	return fmt.Sprintf("%s\t%s\t%s\n",
		time.Now().UTC().Format(time.RFC3339),
		truncate(project, maxFieldLen),
		truncate(query, maxQueryLen),
	)
}
