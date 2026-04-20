package command

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// narrationCtxKey is the context key for the auto-bootstrap narration pointer.
// Placed in this package so internal/mcp can import it without a circular dep.
type narrationCtxKey struct{}

// hintExtrasCtxKey is the context key for the handler-side hint Extras map.
// The MCP/CLI wrapper places an empty map in ctx before the handler runs;
// handlers call HintExtras(ctx)[k] = v to signal rule-relevant side data
// (e.g. quest_clear sets __hints_brief_stale=true when its DB lookup finds
// no recent brief). The wrapper then reads the map when building HintEvent.
type hintExtrasCtxKey struct{}

// mcpWithNarrationPtr returns a child context that carries a *string for
// narration injection. Called by the MCP handler wrapper before each tool
// call; the auto-bootstrap resolver in internal/mcp writes into the pointer
// when it fires.
func mcpWithNarrationPtr(ctx context.Context, ptr *string) context.Context {
	return context.WithValue(ctx, narrationCtxKey{}, ptr)
}

// MCPNarrationPtrFromCtx retrieves the narration pointer from ctx. Exported
// so internal/mcp's auto-bootstrap resolver can write the narration line
// without importing an unexported symbol. Returns nil if not set.
func MCPNarrationPtrFromCtx(ctx context.Context) *string {
	if v := ctx.Value(narrationCtxKey{}); v != nil {
		if ptr, ok := v.(*string); ok {
			return ptr
		}
	}
	return nil
}

// withHintExtras returns a child context carrying a fresh Extras map for
// handler-side hint signals. Called by the MCP/CLI handler wrapper before
// the handler runs.
func withHintExtras(ctx context.Context, m map[string]any) context.Context {
	return context.WithValue(ctx, hintExtrasCtxKey{}, m)
}

// HintExtras returns the mutable Extras map from ctx, or nil if the
// wrapper didn't place one. Handlers use this to signal rule-relevant
// side data without adding new ArgSpec fields. Safe to call with a ctx
// that has no map (returns nil, handlers should guard).
func HintExtras(ctx context.Context) map[string]any {
	if v := ctx.Value(hintExtrasCtxKey{}); v != nil {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	return nil
}

// BindMCP registers c as an MCP tool on server. CLIOnly commands
// return early (no MCP surface).
func (c *Command[I, O]) BindMCP(server *sdkmcp.Server, d Deps) {
	if c.CLIOnly {
		return
	}
	tool, handler := c.buildMCP(d)
	sdkmcp.AddTool(server, tool, handler)
}

// BuildMCPForTest is the test-only accessor for the MCP artifacts
// produced from this Command. Returns the same *Tool that BindMCP
// would register — useful for golden-file snapshots without wiring a
// live sdkmcp.Server.
func (c *Command[I, O]) BuildMCPForTest(d Deps) *sdkmcp.Tool {
	tool, _ := c.buildMCP(d)
	return tool
}

// buildMCP constructs the MCP tool + handler closure. Exposed for
// test-only callers that want to exercise the handler without wiring a
// real server; production callers use BindMCP.
func (c *Command[I, O]) buildMCP(d Deps) (*sdkmcp.Tool, sdkmcp.ToolHandlerFor[I, any]) {
	description := c.Long
	if description == "" {
		description = c.Short
	}
	tool := &sdkmcp.Tool{
		Name:        c.Name,
		Description: description,
	}
	// Generate InputSchema from I. The SDK will auto-infer if we leave
	// InputSchema nil, but we set it explicitly so the ArgSpec.Help text
	// shows up as jsonschema descriptions and so we can drop MCPOnly=false
	// fields the spec wants hidden.
	if schema, err := buildMCPSchema[I](c.Args); err == nil {
		tool.InputSchema = schema
	}

	handler := func(ctx context.Context, _ *sdkmcp.CallToolRequest, in I) (*sdkmcp.CallToolResult, any, error) {
		start := time.Now()
		// handlerErr and respBytes are observed late via pointers so the
		// defer sees the values assigned after the handler body executes.
		var handlerErr error
		var respBytes uint
		if d.RecordTelemetry != nil {
			//nolint:gocritic // ptrToRefParam — defer must observe the late-bound values
			defer d.RecordTelemetry(ctx, c.Name, start, &handlerErr, &respBytes)
		}

		// Narration injection for implicit auto-bootstrap (QUEST-65).
		// Place a *string in ctx before the handler runs. If d.ResolveProj
		// auto-bootstraps (no active project → cwd infer succeeds), it
		// writes the narration line into this pointer. We prepend it to the
		// tool's output after the handler returns so the state transition
		// is visible to the agent and user.
		var narration string
		if d.PrependNarration {
			ctx = mcpWithNarrationPtr(ctx, &narration)
		}

		// Hint extras injection (QUEST-58). Handlers can call
		// HintExtras(ctx) to signal rule-relevant side data without
		// adding new ArgSpec fields.
		var hintExtras map[string]any
		if d.EvaluateHints != nil {
			hintExtras = map[string]any{}
			ctx = withHintExtras(ctx, hintExtras)
		}

		var out O
		out, handlerErr = c.Handler(ctx, d, in)
		if handlerErr != nil {
			return mcpErrorResult(c, MCPSink{}, handlerErr), nil, nil
		}
		if c.MCPFormat == nil {
			handlerErr = fmt.Errorf("%s: MCPFormat missing", c.Name)
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("[fatal] %s: MCPFormat missing", c.Name)}},
				IsError: true,
			}, nil, nil
		}
		body := c.MCPFormat(MCPSink{}, out)

		// Hint evaluation (QUEST-58). Runs AFTER the handler so the
		// engine sees handler-posted Extras and a real IsError signal.
		var hintFire HintFire
		if d.EvaluateHints != nil {
			hintFire = d.EvaluateHints(ctx, HintEvent{
				Tool:    c.Name,
				Args:    reflectArgs(in),
				IsError: handlerErr != nil,
				Extras:  hintExtras,
			})
		}

		// Compose the final body. Ordering (top-down): bolded top-severity
		// fire, auto-bootstrap narration, tool body, muted bottom-severity
		// fire. The narration stays just above the body because it is the
		// immediately-relevant state transition; top-severity hints sit
		// above because agents must see them before anything else.
		if !hintFire.Empty() && hintFire.Top {
			body = hintFire.Rendered + "\n" + body
		}
		if narration != "" {
			body = narration + "\n" + body
		}
		if !hintFire.Empty() && !hintFire.Top {
			body = strings.TrimRight(body, "\n") + "\n" + hintFire.Rendered
		}
		// Record the byte count of the fully-composed body so the deferred
		// RecordTelemetry can log it as resp_bytes in usage.log.
		respBytes = uint(len(body))
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: body}},
		}, nil, nil
	}
	return tool, handler
}

// buildMCPSchema produces a jsonschema.Schema tailored to the command.
// Starts from reflection over I, then:
//
//  1. Deletes any property that an ArgSpec has marked CLIOnly.
//  2. Relaxes FlexInt64 fields so the SDK's pre-unmarshal validation
//     accepts both `42` and `"42"` — mirrors the QUEST-14 fix.
func buildMCPSchema[I any](args []ArgSpec) (*jsonschema.Schema, error) {
	var zero I
	inputType := reflect.TypeOf(zero)
	schema, err := jsonschema.ForType(inputType, &jsonschema.ForOptions{})
	if err != nil {
		return nil, err
	}
	for _, a := range args {
		if a.CLIOnly {
			delete(schema.Properties, a.Name)
			schema.Required = removeString(schema.Required, a.Name)
		}
	}
	relaxFlexIntProperties(schema, inputType)
	return schema, nil
}

func removeString(xs []string, target string) []string {
	out := xs[:0]
	for _, x := range xs {
		if x != target {
			out = append(out, x)
		}
	}
	return out
}

// mcpErrorResult mirrors internal/mcp/errors.toolErrorf for the registry
// path. Kept here (rather than importing internal/mcp) so the command
// package stays free of circular imports with internal/mcp.
func mcpErrorResult[I, O any](c *Command[I, O], sink MCPSink, err error) *sdkmcp.CallToolResult {
	const errPrefix = "[error] "
	var body string
	if c.MCPErrorFormat != nil {
		if msg, ok := c.MCPErrorFormat(sink, err); ok {
			body = strings.TrimRight(msg, "\n")
		}
	}
	if body == "" {
		body = fmt.Sprintf("%s%s: %v", errPrefix, c.Name, err)
	} else if !strings.HasPrefix(body, "[error]") && !strings.HasPrefix(body, "[fatal]") {
		body = errPrefix + body
	}
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: body}},
		IsError: true,
	}
}

// Unused for now; kept to signal intent for QUEST-45 when commands
// start accepting json.RawMessage-shaped arguments.
var _ = json.Marshal
