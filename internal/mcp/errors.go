package mcp

import (
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// errorPrefix is the recoverable-error sentinel agents read from tool
// output. Load-bearing: downstream INSTRUCTIONS + CLAUDE.md point agents
// at "[error] no active project set" as the exact string to recover from.
// Don't change it without coordinating with those docs.
const errorPrefix = "[error]"

// fatalPrefix is the non-recoverable sentinel. Use for "the server is
// misconfigured" / "the DB file is missing" — the class of failures
// where re-bootstrapping won't help and the user must intervene. Agents
// should stop the loop and surface the message verbatim.
const fatalPrefix = "[fatal]"

// toolErrorf constructs a structured tool-call error result suitable for
// return from a [mcp.ToolHandlerFor]. The message is prefixed with
// [error] (recoverable); IsError is set so the agent SDK surfaces this
// as a recoverable tool failure, NOT a protocol error. Recoverable
// errors should include a pointer to the next step in the caller's
// message so agents know how to self-correct.
//
// Returning (result, nil) from a handler is correct here: the SDK
// treats a non-nil Go error as a protocol error, which the client-side
// agent typically cannot recover from. We want the agent to see the
// textual recovery guidance, so we set IsError on the result instead.
func toolErrorf(format string, args ...any) *mcp.CallToolResult {
	msg := fmt.Sprintf(format, args...)
	if !strings.HasPrefix(msg, errorPrefix) && !strings.HasPrefix(msg, fatalPrefix) {
		msg = errorPrefix + " " + msg
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

// toolFatalf is toolErrorf's non-recoverable sibling. Prefixed with
// [fatal]; reserved for "the agent cannot self-correct by retrying".
// Returns the same shape (IsError=true) so the SDK routes it through
// the same channel; the PREFIX tells the agent whether to try again.
func toolFatalf(format string, args ...any) *mcp.CallToolResult {
	msg := fmt.Sprintf(format, args...)
	if !strings.HasPrefix(msg, fatalPrefix) {
		msg = fatalPrefix + " " + msg
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

// textResult wraps a plain string body in a CallToolResult with IsError
// cleared. Handlers that produce structured multi-part content should
// build their own Content slice; this helper is for the common
// "[action done]\n\n<body>" shape used by most handlers.
func textResult(body string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: body}},
	}
}
