package quest

import "strings"

// Note prefixes — the wire contract for task_notes rows. Each system-authored
// note begins with one of these prefixes; free-form agent journal notes have
// none. Replay (loadSpec, Clear report reader, pulse churn count) parses on
// these prefixes, so the writer and reader must agree byte-for-byte.
//
// DO NOT change these strings without a migration plan: existing DB rows use
// the exact values and readers parse them verbatim.
const (
	NotePrefixSpec        = "[spec] "
	NotePrefixSpecReplace = "[spec-replace] "
	NotePrefixRework      = "[rework] of: "
	NotePrefixCheckpoint  = "[checkpoint] "
	NotePrefixCompleted   = "[completed] "
)

// Event kinds — the enum column `event` in task_events. Writers (Post,
// Accept, Clear, Journal, Cascade, Bounties) insert one of these values;
// readers (pulse, scroll, funnel metrics) filter on them. Like the note
// prefixes, these strings are persisted in SQLite — do not change values
// without a migration.
const (
	EventCreated      = "created"
	EventNoted        = "noted"
	EventClaimed      = "claimed"
	EventDone         = "done"
	EventUnblocked    = "unblocked"
	EventPMNextCalled = "pm_next_called"
)

// IsSystemNote reports whether the given note string begins with any known
// system-authored prefix. Used by MCP formatters to distinguish system
// bookkeeping (spec/spec-replace/rework/checkpoint/completed) from free-form
// agent journal entries when deciding which note to surface as "latest
// journal".
func IsSystemNote(note string) bool {
	for _, p := range []string{
		NotePrefixSpec,
		NotePrefixSpecReplace,
		NotePrefixRework,
		NotePrefixCheckpoint,
		NotePrefixCompleted,
	} {
		if strings.HasPrefix(note, p) {
			return true
		}
	}
	return false
}
