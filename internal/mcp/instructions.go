// Package mcp implements the guild MCP stdio server — the agent-facing
// contract. Tool handlers in this package are registered on the
// [modelcontextprotocol/go-sdk] Server and exchange JSON-RPC over stdin/
// stdout; everything else in the binary (lore, quest, storage, session)
// stays transport-agnostic.
//
// The INSTRUCTIONS string delivered to the host at initialize is built
// dynamically at connect time: static contract (embedded instructions.md)
// concatenated with the active project's current principles so agents
// receive the oath wall without an explicit lore_oath call.
package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "embed"

	"github.com/mathomhaus/guild/internal/lore"
)

//go:embed instructions.md
var staticInstructions string

// buildInstructions builds the full INSTRUCTIONS string for one MCP
// connect. The shape is always:
//
//	<static contract>
//
//	## Active Principles (oath wall)
//	- <title> — <summary>
//	…
//
// The static contract is the content of instructions.md, embedded at
// build time (unchanged source-of-truth for the Anthropic prompt-cache
// prefix — it never changes within a session, keeping the cache hit rate
// high). The principles section is appended last so a change between
// sessions only invalidates the tail of the cached string.
//
// When project is empty, or loreDB is nil, or no current principles
// exist for the project, INSTRUCTIONS = static contract only (no
// principles section). This covers:
//   - fresh MCP server starts (no active project yet at initialize time)
//   - multi-project host environments where the active project isn't
//     known until guild_session_start is called
//
// Kind filter: only kind='principle' AND status='current' entries
// are included. Sorted by created_at ASC (oldest first) to maintain a
// stable rendering order across sessions — same order as lore_oath
// reversed (lore.Oath returns DESC; we reverse here for ASC).
//
// Called from buildWithContext in server.go at each MCP server start.
func buildInstructions(ctx context.Context, loreDB *sql.DB, project string) string {
	if strings.TrimSpace(project) == "" || loreDB == nil {
		return staticInstructions
	}

	// lore.Oath returns kind=principle AND status=current, sorted DESC
	// (newest first). The spec for QUEST-57 requires ASC (oldest first)
	// for stable ordering. We reverse the slice in-place.
	entries, err := lore.Oath(ctx, loreDB, project)
	if err != nil || len(entries) == 0 {
		return staticInstructions
	}

	// Reverse to get ASC order (lore.Oath returns DESC).
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	var b strings.Builder
	b.WriteString(staticInstructions)
	b.WriteString("\n\n## Active Principles (oath wall)\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "- %s — %s\n", e.Title, e.Summary)
	}
	return b.String()
}
