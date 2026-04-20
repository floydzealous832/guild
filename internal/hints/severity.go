package hints

import (
	"encoding/json"
	"strings"
)

// Severity is the hint's urgency tier. Four levels — the engine places
// blocker/warning at the TOP of responses (bolded) and hint/fyi at the
// BOTTOM (muted) per ENTRY-29's position+formatting requirement.
//
// v1 launch-set (ENTRY-29) ships only hint + fyi; blocker/warning exist
// to keep the architecture open to future rules without schema churn.
type Severity string

const (
	// SeverityBlocker is the most urgent tier. Bolded, placed at top.
	// No v1 rule uses this.
	SeverityBlocker Severity = "blocker"
	// SeverityWarning is the next-most-urgent tier. Bolded, placed at top.
	// No v1 rule uses this.
	SeverityWarning Severity = "warning"
	// SeverityHint is the advisory tier the 6 "keep" rules fire at. Muted,
	// placed at bottom.
	SeverityHint Severity = "hint"
	// SeverityFYI is the weakest tier the 3 "demote" rules fire at. Muted,
	// placed at bottom.
	SeverityFYI Severity = "fyi"
)

// String returns the string form of s, matching the DB column value.
func (s Severity) String() string { return string(s) }

// IsTop reports whether s renders at the top of the response (bolded).
func (s Severity) IsTop() bool {
	return s == SeverityBlocker || s == SeverityWarning
}

// Emoji returns the emoji glyph associated with s. Matches the gradient
// in ENTRY-29: ❌ / ⚠️ / 💡 / ℹ️.
func (s Severity) Emoji() string {
	switch s {
	case SeverityBlocker:
		return "❌"
	case SeverityWarning:
		return "⚠️"
	case SeverityHint:
		return "💡"
	case SeverityFYI:
		return "ℹ️"
	}
	return ""
}

// Label returns the [label] tag for ASCII renderers (no-emoji mode).
func (s Severity) Label() string {
	switch s {
	case SeverityBlocker:
		return "[blocker]"
	case SeverityWarning:
		return "[warning]"
	case SeverityHint:
		return "[hint]"
	case SeverityFYI:
		return "[fyi]"
	}
	return "[hint]"
}

// Rank is the ordering score used when more than one rule fires on a
// single response — the highest rank wins the one-hint budget slot.
func (s Severity) Rank() int {
	switch s {
	case SeverityBlocker:
		return 4
	case SeverityWarning:
		return 3
	case SeverityHint:
		return 2
	case SeverityFYI:
		return 1
	}
	return 0
}

// ParseSeverity turns a DB string into a Severity. Unknown values fall
// back to SeverityHint (defensive — the schema's CHECK is soft).
func ParseSeverity(s string) Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "blocker":
		return SeverityBlocker
	case "warning":
		return SeverityWarning
	case "hint":
		return SeverityHint
	case "fyi":
		return SeverityFYI
	}
	return SeverityHint
}

// Era identifies which client surface is invoking the engine. Era-aware
// rules (currently only no-brief-24h) use this to pick between stored
// per_era_severity values when they deviate from the base Severity.
type Era string

const (
	// EraMCP denotes an MCP-invoked tool call (mcp_guild era). The default
	// for MCP handlers.
	EraMCP Era = "mcp"
	// EraBash denotes a Bash CLI invocation (bash_cli era).
	EraBash Era = "bash"
)

// ResolveEraSeverity returns the effective severity for baseSeverity under
// era, consulting perEraJSON (the per_era_severity DB column).
//
// perEraJSON is either "" or a JSON object mapping era label → severity
// label, e.g. `{"mcp":"hint","bash":"fyi"}`. Unknown keys or parse errors
// fall back to baseSeverity so a corrupt DB row degrades gracefully.
func ResolveEraSeverity(baseSeverity Severity, era Era, perEraJSON string) Severity {
	s := strings.TrimSpace(perEraJSON)
	if s == "" {
		return baseSeverity
	}
	m := map[string]string{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return baseSeverity
	}
	if v, ok := m[string(era)]; ok {
		return ParseSeverity(v)
	}
	return baseSeverity
}
