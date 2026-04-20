package quest

import (
	"context"
	"fmt"
	"strings"

	"github.com/mathomhaus/guild/internal/command"
)

type GuildInput struct {
	Project string `json:"project,omitempty"`
}

type GuildOutput struct {
	Summary *GuildSummary `json:"summary"`
}

var GuildCommand = &command.Command[GuildInput, GuildOutput]{
	Name:    "quest_guild",
	CLIPath: []string{"quest", "guild"},
	Short:   "per-project summary by campaign",
	Long:    "Per-project quest summary grouped by campaign: counts of next, in-progress, blocked, and done quests.",
	Args: []command.ArgSpec{
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in GuildInput) (GuildOutput, error) {
		db, err := d.OpenDB(ctx)
		if err != nil {
			return GuildOutput{}, err
		}
		defer func() { _ = db.Close() }()
		pid, err := d.ResolveProj(ctx, in.Project)
		if err != nil {
			return GuildOutput{}, err
		}
		summary, err := Guild(ctx, db, pid)
		if err != nil {
			return GuildOutput{}, err
		}
		return GuildOutput{Summary: summary}, nil
	},
	CLIFormat: formatGuildCLI,
	MCPFormat: formatGuildMCP,
}

func formatGuildCLI(s command.CLISink, o GuildOutput) string {
	summary := o.Summary
	var b strings.Builder
	fmt.Fprintf(&b, "project: %s\n\n", summary.ProjectID)
	header := fmt.Sprintf("  %-30s  %5s  %6s  %7s  %5s", "Campaign", "Next", "Active", "Blocked", "Done")
	sep := "  " + strings.Repeat("-", len(header)-2)
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(sep)
	b.WriteString("\n")
	for _, e := range summary.Epics {
		fmt.Fprintf(&b, "  %-30s  %5d  %6d  %7d  %5d\n",
			e.Epic, e.Next, e.InProgress, e.Blocked, e.Done)
	}
	b.WriteString(sep)
	b.WriteString("\n")
	t := summary.Totals
	fmt.Fprintf(&b, "  %-30s  %5d  %6d  %7d  %5d",
		"TOTAL", t.Next, t.InProgress, t.Blocked, t.Done)
	return b.String()
}

func formatGuildMCP(s command.MCPSink, o GuildOutput) string {
	summary := o.Summary
	var b strings.Builder
	b.WriteString(s.Line("🏛️", "", fmt.Sprintf("guild %s:", summary.ProjectID)))
	for _, e := range summary.Epics {
		fmt.Fprintf(&b, "  %s: next=%d active=%d blocked=%d done=%d\n",
			e.Epic, e.Next, e.InProgress, e.Blocked, e.Done)
	}
	t := summary.Totals
	fmt.Fprintf(&b, "  TOTAL: next=%d active=%d blocked=%d done=%d",
		t.Next, t.InProgress, t.Blocked, t.Done)
	return b.String()
}
