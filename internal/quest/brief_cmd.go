package quest

import (
	"context"
	"errors"
	"strings"

	"github.com/mathomhaus/guild/internal/command"
)

type BriefInput struct {
	Text    string `json:"text" jsonschema:"~200-word handoff: what was done, what's next, gotchas"`
	Project string `json:"project,omitempty"`
}

type BriefOutput struct{}

var BriefCommand = &command.Command[BriefInput, BriefOutput]{
	Name:    "quest_brief",
	CLIPath: []string{"quest", "brief"},
	Short:   "write session-end handoff for the next agent",
	Long:    "Session-end briefing for the next agent. Surfaced by next session's guild_session_start. Call before compact.",
	Args: []command.ArgSpec{
		{
			Name:     "text",
			Kind:     command.ArgPositional,
			Type:     command.ArgString,
			Required: true,
			Variadic: true,
			Help:     "~200-word handoff: what was done, what's next, gotchas",
		},
		{
			Name:  "project",
			Short: "p",
			Kind:  command.ArgFlag,
			Type:  command.ArgString,
			Help:  "project override",
		},
	},
	Handler: func(ctx context.Context, d command.Deps, in BriefInput) (BriefOutput, error) {
		if strings.TrimSpace(in.Text) == "" {
			return BriefOutput{}, errors.New("text required")
		}
		db, err := d.OpenDB(ctx)
		if err != nil {
			return BriefOutput{}, err
		}
		defer func() { _ = db.Close() }()

		pid, err := d.ResolveProj(ctx, in.Project)
		if err != nil {
			return BriefOutput{}, err
		}
		if err := Brief(ctx, db, pid, in.Text, "agent"); err != nil {
			return BriefOutput{}, err
		}
		return BriefOutput{}, nil
	},
	CLIFormat: func(s command.CLISink, _ BriefOutput) string { return formatBriefed(s) },
	MCPFormat: func(s command.MCPSink, _ BriefOutput) string { return formatBriefed(s) },
}

func formatBriefed(s lineListSink) string {
	return strings.TrimRight(s.Line("📋", "[briefing]", "briefed for next session"), "\n")
}
