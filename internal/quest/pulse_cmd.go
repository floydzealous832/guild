package quest

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mathomhaus/guild/internal/command"
)

type PulseInput struct {
	WindowDays int    `json:"window_days,omitempty" jsonschema:"window for churn/rework analysis (default 30)"`
	Project    string `json:"project,omitempty"`
}

type PulseOutput struct {
	Report *PulseReport `json:"report"`
	Days   int          `json:"days"`
}

var PulseCommand = &command.Command[PulseInput, PulseOutput]{
	Name:       "quest_pulse",
	CLIPath:    []string{"quest", "pulse"},
	CLIAliases: []string{"health"},
	Short:      "quest quality dashboard: rework, churn, hot files",
	Long:       "Rework rate, churn rate, hot files, untested quests. Surfaces silent rework no one filed.",
	Args: []command.ArgSpec{
		{Name: "window_days", Kind: command.ArgFlag, Type: command.ArgInt, Help: "window days for analysis (default 30)"},
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in PulseInput) (PulseOutput, error) {
		days := in.WindowDays
		if days <= 0 {
			days = 30
		}
		db, err := d.OpenDB(ctx)
		if err != nil {
			return PulseOutput{}, err
		}
		defer func() { _ = db.Close() }()
		pid, err := d.ResolveProj(ctx, in.Project)
		if err != nil {
			return PulseOutput{}, err
		}
		rep, err := Pulse(ctx, db, pid, time.Duration(days)*24*time.Hour)
		if err != nil {
			return PulseOutput{}, err
		}
		return PulseOutput{Report: rep, Days: days}, nil
	},
	CLIFormat: formatPulseCLI,
	MCPFormat: formatPulseMCP,
}

// formatPulseCLI renders the richer human-facing report — preserves
// the multi-line structure (banner + stats block + hot files + hint).
func formatPulseCLI(s command.CLISink, o PulseOutput) string {
	r := o.Report
	if r == nil {
		return "no pulse data"
	}
	var b strings.Builder
	b.WriteString(s.Line("💓", "[pulse]", "guild pulse"))
	b.WriteString("\n")
	if r.ClearedTotal == 0 {
		b.WriteString("no cleared quests yet — quest health needs at least one cleared quest\n")
		return strings.TrimRight(b.String(), "\n")
	}
	fmt.Fprintf(&b, "total quests cleared (all time): %d\n", r.ClearedTotal)
	fmt.Fprintf(&b, "quests cleared in last %dd: %d\n\n", o.Days, r.ClearedInWindow)
	if r.ClearedInWindow == 0 {
		fmt.Fprintf(&b, "no quests cleared in the last %d days\n", o.Days)
		return strings.TrimRight(b.String(), "\n")
	}
	flag := ""
	if r.HighRework {
		flag = "  ← high, investigate"
	}
	fmt.Fprintf(&b, "guild summary (last %dd):\n", o.Days)
	fmt.Fprintf(&b, "  rework rate: %d of %d cleared quests (%d%%)%s\n",
		r.ReworkCount, r.ClearedInWindow, r.ReworkPct, flag)
	if len(r.HotFiles) > 0 {
		hf := r.HotFiles[0]
		extra := ""
		if len(r.HotFiles) > 1 {
			parts := make([]string, 0, len(r.HotFiles)-1)
			for _, h := range r.HotFiles[1:] {
				parts = append(parts, fmt.Sprintf("%s (%d quests)", h.File, h.QuestCount))
			}
			extra = "; " + strings.Join(parts, "; ")
		}
		fmt.Fprintf(&b, "  hot files: %s (touched in %d quests)%s\n", hf.File, hf.QuestCount, extra)
	} else {
		b.WriteString("  hot files: none — no file touched by 2+ quests in window\n")
	}
	fmt.Fprintf(&b, "  median spec-updates per quest: %.1f\n", r.ChurnMedian)
	if r.NoReport > 0 {
		fmt.Fprintf(&b, "\n  hint: %d cleared quest(s) have no --report — pass --report on quest clear.\n", r.NoReport)
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatPulseMCP renders the compact one-liner — preserves old MCP
// output shape.
func formatPulseMCP(s command.MCPSink, o PulseOutput) string {
	r := o.Report
	if r == nil {
		return "📈 no pulse data"
	}
	return fmt.Sprintf("📈 pulse (%dd window): rework=%d/%d (%d%%) · hot files=%d · no-report=%d",
		o.Days, r.ReworkCount, r.ClearedInWindow, r.ReworkPct, len(r.HotFiles), r.NoReport)
}
