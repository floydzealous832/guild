package hints

import (
	"context"
	"fmt"
)

// AutoPruneFloor is the minimum hit rate a rule must maintain to stay at
// its current severity/enabled state. Rules below the floor get demoted
// (hint → fyi) or disabled (fyi → disabled) on the next prune pass.
//
// The value 0.1446 is the 25th percentile of the 9 launch-set rules'
// hit rates on the guild-spike-hints 126-session replay (see ENTRY-29
// and guild-spike-hints/RESULTS.md for the derivation).
//
// Calibration caveat: hit rates are latent compliance ceilings, not
// hint lift. Don't over-read this constant as "hints below 14% are
// bad" — it's "hints below 14% stop contributing positively under the
// current replay corpus". QUEST-61 measures actual lift.
const AutoPruneFloor = 0.1446

// MinScoredBeforePrune is the minimum number of scored fires (i.e.
// followed_through IS NOT NULL) a rule must have before auto-prune
// considers it. Below this, a low hit rate is statistical noise.
const MinScoredBeforePrune = 20

// PruneAction is one prune-pass decision, emitted for logging and the
// guild hints prune CLI's stdout.
type PruneAction struct {
	// RuleID is the rule affected.
	RuleID string
	// Before is the severity the rule had before pruning.
	Before Severity
	// After is the severity the rule has after pruning. Equal to
	// Before when Disabled=true (disabled rules keep their severity).
	After Severity
	// Disabled is true when prune flipped the rule's enabled flag to
	// false (fyi → disabled).
	Disabled bool
	// HitRate is the computed hit rate at prune time.
	HitRate float64
	// Scored is the count of scored fires feeding HitRate.
	Scored int
}

// String is a stable one-line summary for log emission.
func (a PruneAction) String() string {
	if a.Disabled {
		return fmt.Sprintf("hints: prune %s: disabled (hit_rate=%.4f over %d scored fires)",
			a.RuleID, a.HitRate, a.Scored)
	}
	if a.Before != a.After {
		return fmt.Sprintf("hints: prune %s: %s → %s (hit_rate=%.4f over %d scored fires)",
			a.RuleID, a.Before, a.After, a.HitRate, a.Scored)
	}
	return fmt.Sprintf("hints: prune %s: unchanged (hit_rate=%.4f over %d scored fires)",
		a.RuleID, a.HitRate, a.Scored)
}

// Prune walks every rule's Stats and demotes/disables those below the
// AutoPruneFloor. Returns the list of actions taken (empty when nothing
// changed).
//
// Strategy (per ENTRY-29):
//   - hint rule below floor → demote to fyi
//   - fyi rule below floor  → disable (enabled=0)
//   - blocker/warning rules never auto-prune (v1 has none, but the
//     guard keeps future rules safe from a surprise-demotion regression).
//
// Only rules with Scored >= MinScoredBeforePrune are considered — below
// that, a low hit rate is noise.
//
// Prune is safe to call concurrently with Evaluate: it only mutates the
// `hints` table rows, which Evaluate reads on the next LoadRules cycle.
// To pick up prune decisions live, callers should call LoadRules after
// Prune returns.
func (e *Engine) Prune(ctx context.Context) ([]PruneAction, error) {
	if e == nil || e.Store == nil {
		return nil, nil
	}
	stats, err := e.Store.StatsAll(ctx)
	if err != nil {
		return nil, err
	}
	var actions []PruneAction
	for _, st := range stats {
		if !st.Enabled {
			continue // skip already-disabled rules
		}
		if st.Severity == SeverityBlocker || st.Severity == SeverityWarning {
			continue // never auto-prune top-tier rules
		}
		if st.Scored < MinScoredBeforePrune {
			continue // too little data
		}
		if st.HitRate() >= AutoPruneFloor {
			continue // above floor — keep as-is
		}
		action := PruneAction{
			RuleID:  st.RuleID,
			Before:  st.Severity,
			After:   st.Severity,
			HitRate: st.HitRate(),
			Scored:  st.Scored,
		}
		switch st.Severity {
		case SeverityHint:
			action.After = SeverityFYI
			if err := e.Store.SetSeverity(ctx, st.RuleID, SeverityFYI); err != nil {
				return actions, err
			}
		case SeverityFYI:
			action.Disabled = true
			if err := e.Store.SetEnabled(ctx, st.RuleID, false); err != nil {
				return actions, err
			}
		}
		actions = append(actions, action)
		if e.Logger != nil {
			e.Logger.Info(action.String())
		}
	}
	return actions, nil
}
