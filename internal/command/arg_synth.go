package command

import "strings"

// SynthArgValues builds a map[string]any with a non-trivial value per
// ArgSpec entry. The values are chosen to exercise setField paths that
// the default zero-value smoke test misses:
//
//   - ArgString  → "x" (or "QUEST-1" for *_id args that carry quest IDs,
//     or "LORE-1" for lore entry id fields)
//   - ArgInt     → 1
//   - ArgBool    → true
//   - ArgStringSlice → []string{"a"}
//
// CLIOnly specs are skipped because they are stripped from the MCP JSON
// schema by buildMCPSchema and the SDK will reject an unexpected
// property. MCPOnly specs are always included.
//
// Regression gate: this helper was introduced as part of QUEST-53 after
// a runtime panic in lore_meld (commit b6ae7e0, 2026-04-19) where an
// ArgSpec declared Type=ArgString but the input struct field was float64.
// ValidateSpec catches such declaration-time mismatches; this helper
// drives execution-time coverage via TestTools_ArgVariantSmoke so that
// any future setField bugs surface in CI before reaching users.
func SynthArgValues(args []ArgSpec) map[string]any {
	out := make(map[string]any, len(args))
	for _, a := range args {
		if a.CLIOnly {
			continue
		}
		out[a.Name] = synthValue(a)
	}
	return out
}

// synthValue returns one non-trivial value for the given ArgSpec.
func synthValue(a ArgSpec) any {
	switch a.Type {
	case ArgString:
		return synthString(a.Name)
	case ArgInt:
		return 1
	case ArgBool:
		return true
	case ArgStringSlice:
		return []string{"a"}
	default:
		return "x"
	}
}

// synthString picks a domain-appropriate string value for an ArgString
// field. Fields whose name ends in "_id" receive a QUEST-1 placeholder
// (valid ID shape); lore entry id fields (entry_id, from_id, to_id,
// old_id, new_id) receive "LORE-1".
func synthString(name string) string {
	lower := strings.ToLower(name)
	// Lore entry id fields: entry_id and the positional pair args used
	// by lore_link and lore_reforge.
	switch lower {
	case "entry_id", "from_id", "to_id", "old_id", "new_id":
		return "LORE-1"
	}
	// quest_id / rework_of / etc.
	if strings.HasSuffix(lower, "_id") {
		return "QUEST-1"
	}
	return "x"
}
