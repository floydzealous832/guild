package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestShutdownLevelHandler_DemotesServerRunCancelled is QUEST-2's
// regression guard. The upstream SDK (mcp/server.go Run) calls
// Logger.Error("server run cancelled", ...) on SIGINT/SIGTERM — a
// clean shutdown path that should surface as INFO, not ERROR, to
// operators tailing stderr.
func TestShutdownLevelHandler_DemotesServerRunCancelled(t *testing.T) {
	var buf bytes.Buffer
	logger := newLoggerTo(&buf, "json", "debug")

	// SDK's call site passes error=ctx.Err() alongside.
	logger.Error(shutdownMessage, "error", context.Canceled)

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
		t.Fatalf("unmarshal log line: %v (raw: %q)", err, buf.String())
	}
	if got := record["level"]; got != "INFO" {
		t.Errorf("expected demoted level INFO, got %v (full: %q)", got, buf.String())
	}
	if got := record["msg"]; got != shutdownMessage {
		t.Errorf("expected message preserved, got %v", got)
	}
	// The error=... attr must survive the demotion.
	if _, ok := record["error"]; !ok {
		t.Errorf("expected error attr preserved, got %q", buf.String())
	}
}

// TestShutdownLevelHandler_LeavesOtherErrorsAlone guards the
// narrow scoping of the demotion — a real ERROR must still log at
// ERROR so operators can distinguish failures.
func TestShutdownLevelHandler_LeavesOtherErrorsAlone(t *testing.T) {
	var buf bytes.Buffer
	logger := newLoggerTo(&buf, "json", "debug")

	logger.Error("something actually broke", "err", "disk full")

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
		t.Fatalf("unmarshal log line: %v", err)
	}
	if got := record["level"]; got != "ERROR" {
		t.Errorf("unrelated ERROR demoted; want ERROR, got %v", got)
	}
}

// TestShutdownLevelHandler_WithAttrs checks the wrapper preserves
// pre-bound attributes through WithAttrs (used by slog chaining).
func TestShutdownLevelHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := newLoggerTo(&buf, "json", "debug").With("component", "mcp")

	logger.Error(shutdownMessage, "error", context.Canceled)

	out := buf.String()
	if !strings.Contains(out, `"level":"INFO"`) {
		t.Errorf("WithAttrs path lost demotion: %q", out)
	}
	if !strings.Contains(out, `"component":"mcp"`) {
		t.Errorf("WithAttrs path lost bound attrs: %q", out)
	}
}

// Re-import slog to keep "declared and not used" linters happy in
// configurations where the above tests get stripped by build tags.
var _ = slog.LevelError
