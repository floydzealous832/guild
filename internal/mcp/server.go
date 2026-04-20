package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mathomhaus/guild/internal/session"
)

// serverName is the MCP server identity the host sees in its
// `tools/list` response and logs. Lowercase, stable — agents rely on
// the literal string "guild" in tool-prefix disambiguation
// (`mcp__guild__*`).
const serverName = "guild"

// serverVersion is advertised to the host on connect. Bumped manually
// on protocol-visible changes (new tools, INSTRUCTIONS edits, schema
// shifts). Wire to the build-time VCS stamp when release tooling
// matures; until then, hand-updated.
const serverVersion = "0.1.0-dev"

// Serve constructs the guild MCP server and runs it on the stdio
// transport until ctx is cancelled or the client closes the session.
//
// Construction steps, in order:
//
//  1. Build a stderr-bound [slog.Logger] (stdout is the JSON-RPC
//     transport; logging there corrupts the protocol).
//  2. [sdkmcp.NewServer] with the dynamically built INSTRUCTIONS string
//     (static contract + active project's principles) in
//     [sdkmcp.ServerOptions] so the host loads the full discipline
//     contract during `initialize`.
//  3. Register all tools via Register().
//  4. Run over [sdkmcp.StdioTransport] — the SDK handles the JSON-RPC
//     framing, keep-alive, and cancellation plumbing.
//
// Run blocks until ctx is cancelled (via signal.NotifyContext in
// cmd/guild/mcp.go) or the peer closes the connection. A cancelled ctx
// is NOT an error from the server's perspective — the operator asked us
// to stop — so we suppress the resulting ctx.Err() and return nil.
func Serve(ctx context.Context) error {
	log := newLogger()

	// Stale session cleanup runs exactly once per server start.
	// Best-effort: a probe error or missing directory is never fatal —
	// we log the failure and continue serving. The sessions dir may not
	// exist yet on the very first run (CleanupStale treats it as a no-op).
	if err := session.CleanupStale(ctx); err != nil {
		log.Warn("stale session cleanup failed (non-fatal)", "err", err)
	}

	s, err := buildWithContext(ctx)
	if err != nil {
		return err
	}

	if err := s.Run(ctx, &sdkmcp.StdioTransport{}); err != nil {
		// A context-cancelled exit is the expected happy-path shutdown
		// — signal.NotifyContext cancelled us because the operator
		// sent SIGINT/SIGTERM. Returning that error would make the
		// cobra RunE layer print "error: context canceled" to stderr,
		// which misrepresents a clean stop as a failure.
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("mcp: serve: %w", err)
	}
	return nil
}

// BuildForDocs returns a fully constructed MCP server for doc generation.
// cmd/docgen uses it to render docs/generated/mcp.md.
func BuildForDocs() (*sdkmcp.Server, error) {
	return build()
}

// build constructs the server with static-only INSTRUCTIONS. Used by
// tests and BuildForDocs so they don't touch the real lore DB.
func build() (*sdkmcp.Server, error) {
	return buildServerWithInstructions(staticInstructions)
}

// buildWithContext constructs the server with dynamically baked
// INSTRUCTIONS: static contract + current project's principles (if the
// active project can be resolved at connect time). This is the production
// path called from Serve().
//
// Resolution order for the active project:
//  1. Per-PID session file (~/.guild/sessions/<pid>.json) — set by a
//     prior guild_session_start call in the same OS process; empty for a
//     freshly spawned MCP server.
//  2. GUILD_PROJECT env var — operator-level default.
//  3. CWD → git-toplevel auto-inference (same logic as guild_session_start
//     with no args). Covers the common "MCP host launched from project dir"
//     case where the agent hasn't yet called guild_session_start.
//
// If no project can be resolved via any path, INSTRUCTIONS = static
// contract only (graceful fallback per QUEST-57 spec). This is the
// expected state for multi-project hosts where the agent explicitly
// sets the project on first connect.
//
// Principle baking is best-effort: any DB open or query error falls back
// to static-only INSTRUCTIONS so a missing / corrupt lore.db never
// prevents the server from starting.
func buildWithContext(ctx context.Context) (*sdkmcp.Server, error) {
	logger := newLogger()

	instructions := resolveInstructions(ctx, logger)

	return buildServerWithInstructions(instructions)
}

// resolveInstructions tries to build the dynamic INSTRUCTIONS string.
// Falls back to staticInstructions on any resolution or DB error.
// Logs the outcome at debug level for observability (QUEST-57 measurement hook).
func resolveInstructions(ctx context.Context, logger *slog.Logger) string {
	// Step 1+2: check per-PID session file and GUILD_PROJECT env var.
	project, err := session.ResolveForMCP(ctx, "", os.Getenv(guildProjectEnv))
	if err != nil {
		// No project from session file or env — try CWD auto-inference (step 3).
		// inferProjectFromCWD returns (projectID, viaWorktreeFallback, err); the
		// fallback flag is only material to handleSessionStart's narration, not
		// to INSTRUCTIONS building — discard it here.
		inferred, _, ierr := inferProjectFromCWD(ctx)
		if ierr != nil {
			// All resolution paths exhausted — static-only fallback.
			logger.Debug("mcp: instructions: no active project at connect; using static-only INSTRUCTIONS",
				"session_err", err, "infer_err", ierr)
			return staticInstructions
		}
		project = inferred
	}

	loreDB, dbErr := openLoreDB(ctx)
	if dbErr != nil {
		logger.Debug("mcp: instructions: could not open lore DB; using static-only INSTRUCTIONS",
			"project", project, "err", dbErr)
		return staticInstructions
	}
	defer func() { _ = loreDB.Close() }()

	instructions := buildInstructions(ctx, loreDB, project)

	// Measurement hook (QUEST-57): log built INSTRUCTIONS length and
	// principle count at debug level so operators can quantify actual
	// per-session cost post-ship. Enable with GUILD_MCP_LOG_LEVEL=debug.
	principleCount := strings.Count(instructions[len(staticInstructions):], "\n- ")
	logger.Debug("mcp: instructions: built dynamic INSTRUCTIONS",
		"project", project,
		"instructions_bytes", len(instructions),
		"principle_count", principleCount,
	)

	return instructions
}

// buildServerWithInstructions is the shared server-construction core.
// Called by both build() (static-only, test-friendly) and buildWithContext
// (dynamic, production path). Separated so the two call sites share
// identical server + tool registration without duplication.
func buildServerWithInstructions(instructions string) (*sdkmcp.Server, error) {
	logger := newLogger()

	s := sdkmcp.NewServer(
		&sdkmcp.Implementation{
			Name:    serverName,
			Version: serverVersion,
		},
		&sdkmcp.ServerOptions{
			// Instructions is the agent-visible contract loaded at
			// initialize. See instructions.go for the journal of
			// adjustments.
			Instructions: instructions,
			// Logger routes SDK-internal events (session connect,
			// handler panics, malformed messages) through our
			// stderr-bound slog handler. Stdout remains JSON-RPC
			// only.
			Logger: logger,
		},
	)

	// Register all tools (bootstrap + always-on). See register.go.
	Register(s)

	return s, nil
}
