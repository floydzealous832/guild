package hints

import (
	"context"

	"github.com/mathomhaus/guild/internal/command"
)

// Bridge wires an Engine into the command package's EvaluateHintsFunc
// shape so internal/mcp + internal/cli can set Deps.EvaluateHints without
// importing this package's Engine type directly.
//
// Returns a closure with a *command.HintFire-compatible signature. The
// closure is safe for concurrent use (delegates to Engine.Evaluate which
// is itself concurrent-safe).
//
// Usage:
//
//	eng := hints.NewEngine(store, sessionID, era)
//	_ = eng.LoadRules(ctx)
//	deps.EvaluateHints = hints.Bridge(eng)
func Bridge(eng *Engine) command.EvaluateHintsFunc {
	if eng == nil {
		return nil
	}
	return func(ctx context.Context, ev command.HintEvent) command.HintFire {
		fire := eng.Evaluate(ctx, CallEvent{
			Tool:    ev.Tool,
			Args:    ev.MergedArgs(),
			IsError: ev.IsError,
		})
		if fire.Empty() {
			return command.HintFire{}
		}
		return command.HintFire{
			RuleID:   fire.RuleID,
			Rendered: fire.Render(),
			Top:      fire.Top,
		}
	}
}
