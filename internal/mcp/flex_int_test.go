package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestFlexInt64_UnmarshalJSON covers the three wire forms a client
// might send for a numeric id: bare integer, integer-as-string, and
// null. QUEST-14 is specifically about the string form.
func TestFlexInt64_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"int form", `42`, 42, false},
		{"string form", `"42"`, 42, false},
		{"negative int", `-7`, -7, false},
		{"negative string", `"-7"`, -7, false},
		{"zero", `0`, 0, false},
		{"null", `null`, 0, false},
		{"empty string", `""`, 0, false},
		{"float rejected", `3.14`, 0, true},
		{"non-numeric string", `"abc"`, 0, true},
		{"bool rejected", `true`, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got flexInt64
			err := json.Unmarshal([]byte(tt.input), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v; wantErr %v", err, tt.wantErr)
			}
			if err == nil && got.Int64() != tt.want {
				t.Errorf("got %d; want %d", got.Int64(), tt.want)
			}
		})
	}
}

// TestFlexInt64_MarshalJSON checks that the symmetric marshal path
// emits a number (not a string) so downstream JSON consumers treat it
// normally.
func TestFlexInt64_MarshalJSON(t *testing.T) {
	v := flexInt64(542)
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(out) != "542" {
		t.Errorf("Marshal = %q; want %q", string(out), "542")
	}
}

// TestFlexIntSchema_RelaxesIntegerProperties verifies that the
// generated schema for a struct containing flexInt64 fields lists
// both "integer" and "string" as acceptable JSON types, while
// non-flexInt64 fields stay strictly typed.
func TestFlexIntSchema_RelaxesIntegerProperties(t *testing.T) {
	type sample struct {
		EntryID flexInt64 `json:"entry_id"`
		Name    string    `json:"name"`
		Count   int64     `json:"count"` // NOT flexInt64 — must stay integer-only
	}
	schema := flexIntSchema(reflect.TypeOf(sample{}))

	entryProp, ok := schema.Properties["entry_id"]
	if !ok {
		t.Fatalf("entry_id property missing from schema: %+v", schema.Properties)
	}
	if entryProp.Type != "" {
		t.Errorf("entry_id still has single Type %q; expected Types slice", entryProp.Type)
	}
	gotTypes := append([]string(nil), entryProp.Types...)
	if len(gotTypes) != 2 {
		t.Fatalf("entry_id Types len %d; want 2 (integer+string)", len(gotTypes))
	}
	hasInt, hasStr := false, false
	for _, ty := range gotTypes {
		switch ty {
		case "integer":
			hasInt = true
		case "string":
			hasStr = true
		}
	}
	if !hasInt || !hasStr {
		t.Errorf("entry_id Types = %v; want both integer and string", gotTypes)
	}

	// Non-flexInt64 field must NOT be relaxed.
	countProp, ok := schema.Properties["count"]
	if !ok {
		t.Fatalf("count property missing")
	}
	if len(countProp.Types) != 0 {
		t.Errorf("count unexpectedly relaxed to Types %v", countProp.Types)
	}
}

// TestLoreStudyTool_AcceptsStringEntryID is the end-to-end regression
// guard for QUEST-14: round-trip a `{"entry_id": "42"}` call through
// the registered tool and verify the SDK's schema validation doesn't
// reject it before reaching our handler.
//
// We intentionally pass a missing-project so the handler errors
// cleanly after schema validation passes — the proof of fix is
// specifically the absence of a pre-handler "validating arguments:
// got string, want integer" rejection.
func TestLoreStudyTool_AcceptsStringEntryID(t *testing.T) {
	isolateHome(t)
	s, err := build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	_, client, cleanup := connectInMemory(t, s)
	defer cleanup()

	// First activate a project so we get past resolveProject.
	if _, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "guild_session_start",
		Arguments: map[string]any{"project": "flexintproj"},
	}); err != nil {
		t.Fatalf("session_start: %v", err)
	}

	// String-form entry_id — the case QUEST-14 is fixing.
	res, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "lore_study",
		Arguments: map[string]any{"entry_id": "999"},
	})
	if err != nil {
		t.Fatalf("CallTool(lore_study, entry_id='999'): %v", err)
	}
	body := textOf(res.Content)
	if strings.Contains(body, "got string, want integer") ||
		strings.Contains(body, "validating") && strings.Contains(body, "arguments") {
		t.Errorf("string entry_id rejected at schema-validation layer: %q", body)
	}
	// The handler itself will error with "ENTRY-999 not found" (or
	// similar) — that's expected and confirms we reached it.
	if !strings.Contains(body, "999") && !strings.Contains(body, "not found") &&
		!strings.Contains(body, "lore_study") {
		t.Errorf("handler didn't run on string id; body: %q", body)
	}
}

// TestLoreStudyTool_AcceptsIntEntryID confirms we didn't break the
// original numeric path.
func TestLoreStudyTool_AcceptsIntEntryID(t *testing.T) {
	isolateHome(t)
	s, err := build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	_, client, cleanup := connectInMemory(t, s)
	defer cleanup()

	if _, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "guild_session_start",
		Arguments: map[string]any{"project": "flexintproj"},
	}); err != nil {
		t.Fatalf("session_start: %v", err)
	}

	res, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "lore_study",
		Arguments: map[string]any{"entry_id": 999},
	})
	if err != nil {
		t.Fatalf("CallTool(lore_study, entry_id=999): %v", err)
	}
	body := textOf(res.Content)
	if strings.Contains(body, "validating") && strings.Contains(body, "arguments") {
		t.Errorf("numeric entry_id rejected: %q", body)
	}
}
