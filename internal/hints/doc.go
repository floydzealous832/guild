// Package hints is guild's SQL-backed, self-grading hint engine.
//
// The engine fires short advisory lines ("💡 hint …" / "ℹ️ fyi …") on top
// of tool responses when the current call matches a calibrated trigger
// rule. Every fire is recorded to hint_fires, and a follow-through pass
// later scores whether the agent took the suggested action — the per-rule
// hit rate drives an auto-prune loop that disables rules which fall below
// the calibrated 14.46% floor from ENTRY-29 (see guild-spike-hints
// RESULTS.md for the derivation).
//
// # High level
//
// Each MCP/CLI tool response flows through Engine.Evaluate. The engine:
//
//  1. Builds a CallEvent from the tool invocation (name, args, output, err).
//  2. Tracks the event on the session-scoped Context so later rules can see
//     recent history (budget, cooldown, contextual suppression all need it).
//  3. For each enabled Rule whose trigger_tool matches, calls
//     Rule.Trigger(ctx, event). A rule returns (fire=true) when its
//     trigger condition holds.
//  4. Applies engine-wide gates: 1 hint per response cap, per-session cap
//     on fyi, cooldown window, contextual suppression (skip if the
//     suggested follow-through already happened in the last 5 calls),
//     era-aware severity selection (MCP vs Bash CLI).
//  5. Renders the winning hint via the configured Renderer and records
//     the fire to the hint_fires table.
//
// After N more guild events in the same session, Engine.ScoreFollowThroughs
// walks pending fires and writes followed_through=1 if the rule's
// FollowThrough detector found a matching action in the subsequent window.
//
// # Architecture
//
//	       +-------------------------------------------------------+
//	       |                    Engine (stateless-ish)             |
//	       |                                                       |
//	       |   Evaluate(ctx, ev) -> string  (the hint line or "")  |
//	       |                                                       |
//	       |   budget + cooldown + suppression + era selection     |
//	       +------------+---------------------+--------------------+
//	                    |                     |
//	                    v                     v
//	               +---------+           +-----------+
//	               | Context |           |  Store    |
//	               |         |           |           |
//	               |  per    |           | hints /   |
//	               |  PID    |           | hint_fires|
//	               +---------+           +-----------+
//
//	Rule (per launch-set bullet from ENTRY-29):
//	  - ID            e.g. "inscribe-looks-like-quest"
//	  - TriggerTool   e.g. "lore_inscribe"
//	  - Severity      "hint" / "fyi" (blocker/warning supported for future)
//	  - Template      "…"
//	  - Trigger       func(Context, CallEvent) bool
//	  - FollowThrough func(Context, CallEvent) bool  // later events
//
// # Concurrency
//
// An Engine may be called concurrently from multiple goroutines. Internal
// state guarded by a sync.Mutex: the session Context, the per-rule
// cooldown counters, the pending-fire follow-through tracking list.
//
// # Storage
//
// Hints + hint_fires live in quest.db (see 001_init.up.sql). quest.db
// already carries task_events which is the sibling audit log; co-locating
// hint fires there keeps cross-DB joins unnecessary for the prune loop.
// lore.db gets the same tables from the shared migration corpus but the
// engine never writes to them.
//
// # Calibration
//
// The 14.46% auto-prune floor is the 25th percentile of the 9 launch-set
// rules' hit rates on the 126-session replay in guild-spike-hints/
// RESULTS.md. See ENTRY-29 (decision, topic=hint-engine) for the full
// calibration narrative. Hit rates are LATENT COMPLIANCE CEILINGS, not
// hint lift — don't over-index on them as proof of effectiveness until
// QUEST-61's A/B measurement lands.
package hints
