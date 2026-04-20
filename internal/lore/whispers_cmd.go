package lore

import (
	"context"
	"fmt"
	"strings"

	"github.com/mathomhaus/guild/internal/command"
)

type WhispersInput struct {
	Topic   string `json:"topic,omitempty" jsonschema:"filter by topic"`
	Project string `json:"project,omitempty"`
}

type WhispersOutput struct {
	Entries []*Entry `json:"entries"`
}

var WhispersCommand = &command.Command[WhispersInput, WhispersOutput]{
	Name:       "lore_whispers",
	CLIPath:    []string{"lore", "whispers"},
	CLIAliases: []string{"ideas"},
	Short:      "show current idea pipeline (kind=idea, status=seed|exploring)",
	Long:       "List lore entries in the idea pipeline — entries with kind=idea and an early status. Useful for surfacing unfinished threads at session-start.",
	Args: []command.ArgSpec{
		{Name: "topic", Kind: command.ArgFlag, Type: command.ArgString, Help: "filter by topic"},
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in WhispersInput) (WhispersOutput, error) {
		db, err := d.OpenDB(ctx)
		if err != nil {
			return WhispersOutput{}, err
		}
		defer func() { _ = db.Close() }()
		pid, err := d.ResolveProj(ctx, in.Project)
		if err != nil {
			return WhispersOutput{}, err
		}
		entries, err := Whispers(ctx, db, pid, in.Topic)
		if err != nil {
			return WhispersOutput{}, err
		}
		return WhispersOutput{Entries: entries}, nil
	},
	CLIFormat: func(s command.CLISink, o WhispersOutput) string { return formatWhispers(s, o) },
	MCPFormat: func(s command.MCPSink, o WhispersOutput) string { return formatWhispers(s, o) },
}

func formatWhispers(s lineSink, o WhispersOutput) string {
	if len(o.Entries) == 0 {
		return strings.TrimRight(s.Line("💭", "[whispers]", "no whispers in the guild"), "\n")
	}
	var b strings.Builder
	b.WriteString(s.Line("💭", "[whispers]", fmt.Sprintf("%d whisper(s) in the guild:", len(o.Entries))))
	for _, e := range o.Entries {
		fmt.Fprintf(&b, "  %s  [%s]  %s\n", formatEntryID(e.ID), e.Status, e.Title)
		summary := e.Summary
		if len(summary) > 120 {
			summary = summary[:120] + "…"
		}
		if summary != "" {
			fmt.Fprintf(&b, "    %s\n", summary)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
