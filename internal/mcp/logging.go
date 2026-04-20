package mcp

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// logFormatEnv names the environment variable that chooses the slog
// handler for the MCP server process. Values (case-insensitive):
//
//	"json"   — [slog.JSONHandler] (production default)
//	"text"   — [slog.TextHandler] (developer mode)
//
// Any other value (including empty) falls back to JSON: the MCP server
// uses JSON in production and text for dev (env-controlled).
const logFormatEnv = "GUILD_MCP_LOG_FORMAT"

// logLevelEnv names the environment variable that chooses the slog
// minimum level. Recognized values: "debug", "info", "warn", "error".
// Empty / unrecognized defaults to Info.
const logLevelEnv = "GUILD_MCP_LOG_LEVEL"

// newLogger builds the slog.Logger the MCP server hands to
// [mcp.ServerOptions.Logger]. All logging must go through this logger —
// NEVER fmt.Println from a tool handler, because stdout is the JSON-RPC
// transport in stdio mode and any bare write corrupts the protocol.
//
// The writer is always os.Stderr so logs and protocol messages never
// collide. Tests can override via newLoggerTo when asserting log
// contents.
func newLogger() *slog.Logger {
	return newLoggerTo(os.Stderr, os.Getenv(logFormatEnv), os.Getenv(logLevelEnv))
}

// newLoggerTo is the injectable variant used by tests. Kept package-
// private — production callers use newLogger which resolves env vars
// and writes to stderr.
func newLoggerTo(w io.Writer, format, level string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLogLevel(level)}
	var base slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "text":
		base = slog.NewTextHandler(w, opts)
	default: // "json", "", or unrecognized
		base = slog.NewJSONHandler(w, opts)
	}
	return slog.New(&shutdownLevelHandler{wrapped: base})
}

// shutdownLevelHandler wraps a base slog.Handler and rewrites the
// severity of the SDK's "server run cancelled" message from ERROR to
// INFO. The upstream go-sdk (v1.5.0 mcp/server.go Run) calls
// Logger.Error on context cancellation — but in guild's world, a
// SIGINT/SIGTERM driven shutdown is the expected clean-stop path, not
// a failure. Everything else passes through unchanged.
//
// Scoped to this single message so future upstream ERRORs don't get
// accidentally masked. See QUEST-2.
type shutdownLevelHandler struct {
	wrapped slog.Handler
}

// shutdownMessage is the exact SDK log text we demote. Pinned against
// a specific upstream version (go-sdk v1.5.0). If upstream changes the
// wording, Logger.Error will resume firing at ERROR — a loud failure
// we'd rather have than a silent drift into the wrong level.
const shutdownMessage = "server run cancelled"

// Enabled delegates to the wrapped handler. Level-gating happens here
// using the base handler's configured threshold.
func (h *shutdownLevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.wrapped.Enabled(ctx, level)
}

// Handle intercepts the shutdown record and rewrites its Level before
// forwarding. Any other record passes through verbatim.
func (h *shutdownLevelHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level == slog.LevelError && r.Message == shutdownMessage {
		// Build a fresh record at INFO — slog.Record values are
		// copy-on-write safe to clone via NewRecord + AddAttrs.
		demoted := slog.NewRecord(r.Time, slog.LevelInfo, r.Message, r.PC)
		r.Attrs(func(a slog.Attr) bool {
			demoted.AddAttrs(a)
			return true
		})
		return h.wrapped.Handle(ctx, demoted)
	}
	return h.wrapped.Handle(ctx, r)
}

// WithAttrs propagates pre-bound attributes through the wrap.
func (h *shutdownLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &shutdownLevelHandler{wrapped: h.wrapped.WithAttrs(attrs)}
}

// WithGroup propagates groups through the wrap.
func (h *shutdownLevelHandler) WithGroup(name string) slog.Handler {
	return &shutdownLevelHandler{wrapped: h.wrapped.WithGroup(name)}
}

// parseLogLevel maps a GUILD_MCP_LOG_LEVEL string to a [slog.Level].
// Unknown values silently fall through to Info — the server shouldn't
// fail to start over a typo'd env var, and the default covers the
// operational case.
func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
