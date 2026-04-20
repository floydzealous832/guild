package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// flexInt64 is a JSON-flexible int64 that accepts both numeric and
// string-encoded integer forms in tool arguments.
//
// Background (QUEST-14): some MCP clients serialize tool argument
// values as strings even when the tool's JSON Schema declares a numeric
// type — Claude Code's web-tier models, Codex when a tool call goes
// through a text-based transform, etc. The upstream go-sdk runs strict
// JSON Schema validation before unmarshal, so an argument value like
// "542" against an int64 field is rejected with
// "validating arguments: got string, want integer".
//
// flexInt64 solves that in two halves:
//
//  1. A custom UnmarshalJSON that accepts both `42` and `"42"` forms.
//  2. When the tool uses flexInt64 for an ID field, the registration
//     code in register.go sets an explicit InputSchema on the Tool that
//     uses JSON Schema's `type: ["integer","string"]` (with a digit-only
//     pattern on the string form) so the SDK's pre-unmarshal validation
//     lets both forms through.
//
// Use flexInt64 only for input-parameter fields; internal storage stays
// as Go int64 / database-native types.
type flexInt64 int64

// UnmarshalJSON accepts either a JSON number or a JSON string whose
// content parses as a base-10 signed integer. Floats are rejected.
// Empty strings are treated as 0 to make optional fields ergonomic
// (the handler still validates positivity before DB calls).
func (f *flexInt64) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*f = 0
		return nil
	}
	// String form: "42" or `"42"`.
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return fmt.Errorf("flexInt64: unmarshal string: %w", err)
		}
		if s == "" {
			*f = 0
			return nil
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("flexInt64: parse %q as int: %w", s, err)
		}
		*f = flexInt64(n)
		return nil
	}
	// Numeric form. json.Number via Decoder so we reject floats.
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("flexInt64: decode number: %w", err)
	}
	num, ok := raw.(json.Number)
	if !ok {
		return fmt.Errorf("flexInt64: expected number, got %T", raw)
	}
	n, err := num.Int64()
	if err != nil {
		return fmt.Errorf("flexInt64: non-integer number %q: %w", num.String(), err)
	}
	*f = flexInt64(n)
	return nil
}

// MarshalJSON emits the value as a JSON number. We never emit the
// string form back to the client — the flexibility is input-only.
func (f flexInt64) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(int64(f), 10)), nil
}

// Int64 returns the value as a native int64. Handlers call this to
// convert to the type the downstream lore/quest packages expect.
func (f flexInt64) Int64() int64 { return int64(f) }
