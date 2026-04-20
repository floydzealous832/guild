package quest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mathomhaus/guild/internal/command"
)

type ClearInput struct {
	QuestID string `json:"quest_id" jsonschema:"QUEST-NNN to mark complete"`
	Report  string `json:"report" jsonschema:"specific completion report: commit hash, files, issues — REQUIRED"`
	Project string `json:"project,omitempty"`
}

type ClearOutput struct {
	Result    *ClearResult `json:"result"`
	BriefHint string       `json:"brief_hint,omitempty"`
}

var ClearCommand = &command.Command[ClearInput, ClearOutput]{
	Name:    "quest_clear",
	CLIPath: []string{"quest", "clear"},
	Short:   "complete a quest (cascades unblock)",
	Long:    "Complete a quest. Report is REQUIRED — commit hash, files, remaining issues. Cascades unblock dependent quests.",
	Args: []command.ArgSpec{
		{
			Name:     "quest_id",
			Kind:     command.ArgPositional,
			Type:     command.ArgString,
			Required: true,
			Help:     "QUEST-NNN to mark complete",
		},
		{
			Name: "report",
			Kind: command.ArgFlag,
			Type: command.ArgString,
			// Not Required at the domain layer — Clear() accepts empty
			// report and just writes the `done` event without a
			// [completed] note. Encourage callers to pass one via Help
			// phrasing, don't reject the call.
			Help: "completion report: commit hash, files, remaining issues",
		},
		{
			Name:  "project",
			Short: "p",
			Kind:  command.ArgFlag,
			Type:  command.ArgString,
			Help:  "project override",
		},
	},
	Handler: func(ctx context.Context, d command.Deps, in ClearInput) (ClearOutput, error) {
		if strings.TrimSpace(in.QuestID) == "" {
			return ClearOutput{}, errors.New("quest_id required")
		}
		db, err := d.OpenDB(ctx)
		if err != nil {
			return ClearOutput{}, err
		}
		defer func() { _ = db.Close() }()

		pid, err := d.ResolveProj(ctx, in.Project)
		if err != nil {
			return ClearOutput{}, err
		}
		res, err := Clear(ctx, db, pid, in.QuestID, in.Report)
		if err != nil {
			return ClearOutput{}, err
		}

		out := ClearOutput{Result: res}

		// Advisory hint: if no brief has been written recently, remind the
		// caller to write a handoff before compacting. This is shipped via
		// two paths, live together during the QUEST-58 migration window:
		//
		//   1. HintExtras signal — the hint engine's no-brief-24h rule reads
		//      __hints_brief_stale and fires the canonical 💡 line. Preferred.
		//   2. out.BriefHint — legacy field; still populated so CLI paths
		//      that don't yet route through the engine (and backward-compat
		//      consumers of ClearOutput) keep their hint. The MCP format
		//      function drops it when the engine is wired so we don't
		//      render twice.
		lastAt, briefErr := LastBriefAt(ctx, db, pid)
		stale := briefErr == nil && (lastAt.IsZero() || time.Now().Sub(lastAt) > briefStaleThreshold)
		if stale {
			if extras := command.HintExtras(ctx); extras != nil {
				extras["__hints_brief_stale"] = true
			} else {
				// No engine wired (test or cold-path CLI surface) — fall
				// back to the legacy field so output stays consistent.
				out.BriefHint = `no quest_brief yet this session — consider quest_brief("what was done, what's next") before compact`
			}
		}

		return out, nil
	},
	CLIFormat:      func(s command.CLISink, o ClearOutput) string { return formatCleared(s, o) },
	MCPFormat:      func(s command.MCPSink, o ClearOutput) string { return formatCleared(s, o) },
	CLIErrorFormat: func(s command.CLISink, err error) (string, bool) { return formatClearError(s, err) },
	MCPErrorFormat: func(s command.MCPSink, err error) (string, bool) { return formatClearError(s, err) },
}

func formatCleared(s lineListSink, o ClearOutput) string {
	res := o.Result
	var b strings.Builder
	b.WriteString(s.Line("🏆", "[cleared]", fmt.Sprintf("cleared %s", res.Cleared.ID)))
	if len(res.Unblocked) > 0 {
		b.WriteString("  unblocked:\n")
		for _, q := range res.Unblocked {
			if q.Subject != "" {
				fmt.Fprintf(&b, "    - %s: %s\n", q.ID, q.Subject)
				continue
			}
			fmt.Fprintf(&b, "    - %s\n", q.ID)
		}
	}
	if o.BriefHint != "" {
		b.WriteString(s.Line("💡", "[hint]", o.BriefHint))
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatClearError(s lineListSink, err error) (string, bool) {
	if errors.Is(err, ErrNotFound) {
		msg := fmt.Sprintf("quest_clear: %v", err)
		return strings.TrimRight(s.Line("❌", "[err]", msg), "\n"), true
	}
	return "", false
}
