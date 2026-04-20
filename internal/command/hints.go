package command

import (
	"context"
	"reflect"
	"strings"
)

// HintEvent is the payload the MCP/CLI wrappers hand to Deps.EvaluateHints
// after a handler runs. Kept package-local (no internal/hints import) so
// internal/hints can import internal/command if it ever needs to, without
// a cycle.
//
// Shape is deliberately minimal: tool name, a flattened args map built via
// reflection from the handler's typed input I, a bool for the handler
// error state, and optional extras the handler stuffs in for rules that
// need richer context (the no-brief-24h rule for example needs to signal
// "DB check says the session is brief-stale").
type HintEvent struct {
	// Tool is the MCP wire name / CLI subcommand name, e.g. "lore_inscribe".
	Tool string
	// Args is the flattened, string-keyed view of the handler's input.
	// Values are the raw Go types (string/int/bool/[]string/...). Rules
	// read known keys by name.
	Args map[string]any
	// IsError is true when the handler returned a non-nil error.
	IsError bool
	// Extras is an open-ended bag rules can consume without forcing new
	// ArgSpec fields. Example: quest_clear writes `__hints_brief_stale`
	// so the no-brief-24h rule doesn't redo the DB check.
	Extras map[string]any
}

// MergedArgs returns a copy of Args with Extras overlaid. Extras wins on
// key collisions. Used by rule detectors that want one combined view.
func (h HintEvent) MergedArgs() map[string]any {
	if len(h.Extras) == 0 {
		return h.Args
	}
	m := make(map[string]any, len(h.Args)+len(h.Extras))
	for k, v := range h.Args {
		m[k] = v
	}
	for k, v := range h.Extras {
		m[k] = v
	}
	return m
}

// HintFire is the wrapper-visible view of a rule fire. Surface-specific
// renderers (MCP prepends, CLI appends) format the line from this struct.
type HintFire struct {
	// RuleID identifies which rule fired.
	RuleID string
	// Rendered is the ready-to-print hint line (emoji + [label] + message,
	// or ASCII fallback), produced by the hints package.
	Rendered string
	// Top is true for blocker/warning-tier fires that should be placed
	// above the tool's body; false for hint/fyi (below).
	Top bool
}

// Empty reports whether no hint fired. Zero-value HintFire is the
// "no fire" sentinel the MCP/CLI wrappers check before formatting.
func (f HintFire) Empty() bool { return f.RuleID == "" }

// EvaluateHintsFunc is the callback signature Deps.EvaluateHints uses.
// The hints engine returns a single HintFire (or zero-value HintFire
// when no rule fired). Errors are swallowed by the engine side — hint
// evaluation must never break a tool call.
type EvaluateHintsFunc func(ctx context.Context, ev HintEvent) HintFire

// reflectArgs flattens a typed handler input I into a string-keyed map
// using the `json` struct tag for the key name (falls back to the
// lowercase field name). The MCP wrapper uses this to build HintEvent.Args
// without each Command[I,O] writing its own shim.
//
// Unexported fields are skipped; zero-value pointers/slices are included
// as nil so rules can distinguish "unset" from "set to empty".
//
// Only top-level fields are extracted — nested structs are returned as
// whatever Go type they resolve to, and rule detectors can type-assert.
func reflectArgs(in any) map[string]any {
	out := map[string]any{}
	if in == nil {
		return out
	}
	v := reflect.ValueOf(in)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return out
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return out
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := argName(f)
		if name == "" {
			continue
		}
		out[name] = v.Field(i).Interface()
	}
	return out
}

// argName resolves the key to use for a struct field in HintEvent.Args.
// Honors the `json:"name,omitempty"` tag convention used across guild's
// input types. Empty or "-" tag → skip.
func argName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	if tag == "" {
		return strings.ToLower(f.Name)
	}
	if idx := strings.Index(tag, ","); idx >= 0 {
		tag = tag[:idx]
	}
	if tag == "" {
		return strings.ToLower(f.Name)
	}
	return tag
}
