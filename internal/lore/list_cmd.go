package lore

import (
	"context"
	"fmt"
	"strings"

	"github.com/mathomhaus/guild/internal/command"
)

type ListInput struct {
	Topic   string `json:"topic,omitempty" jsonschema:"filter by topic slug"`
	Kind    string `json:"kind,omitempty" jsonschema:"filter by kind"`
	Status  string `json:"status,omitempty" jsonschema:"filter by status"`
	Project string `json:"project,omitempty"`
}

type ListCmdOutput struct {
	Entries []*Entry `json:"entries"`
}

var ListCommand = &command.Command[ListInput, ListCmdOutput]{
	Name:    "lore_list",
	CLIPath: []string{"lore", "list"},
	Short:   "browse entries with optional filters",
	Long:    "Browse entries with optional filters. Use for inventory; lore_appraise for search.",
	Args: []command.ArgSpec{
		{Name: "topic", Kind: command.ArgFlag, Type: command.ArgString, Help: "filter by topic slug"},
		{Name: "kind", Kind: command.ArgFlag, Type: command.ArgString, Help: "filter by kind"},
		{Name: "status", Kind: command.ArgFlag, Type: command.ArgString, Help: "filter by status"},
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in ListInput) (ListCmdOutput, error) {
		db, err := d.OpenDB(ctx)
		if err != nil {
			return ListCmdOutput{}, err
		}
		defer func() { _ = db.Close() }()
		pid, err := d.ResolveProj(ctx, in.Project)
		if err != nil {
			return ListCmdOutput{}, err
		}
		entries, err := List(ctx, db, ListFilters{
			Project: pid,
			Topic:   in.Topic,
			Kind:    Kind(in.Kind),
			Status:  Status(in.Status),
		})
		if err != nil {
			return ListCmdOutput{}, err
		}
		return ListCmdOutput{Entries: entries}, nil
	},
	CLIFormat: func(s command.CLISink, o ListCmdOutput) string { return formatLoreList(s, o) },
	MCPFormat: func(s command.MCPSink, o ListCmdOutput) string { return formatLoreList(s, o) },
}

func formatLoreList(s lineSink, o ListCmdOutput) string {
	if len(o.Entries) == 0 {
		return "no entries"
	}
	var b strings.Builder
	b.WriteString(s.Line("📜", "[list]", fmt.Sprintf("%d entry(ies):", len(o.Entries))))
	for _, e := range o.Entries {
		fmt.Fprintf(&b, "  %s [%s · %s]  %s\n", formatEntryID(e.ID), e.Kind, e.Status, e.Title)
	}
	return strings.TrimRight(b.String(), "\n")
}
