package lore

import (
	"context"
	"fmt"
	"strings"

	"github.com/mathomhaus/guild/internal/command"
)

type OathInput struct {
	Project string `json:"project,omitempty"`
}

type OathOutput struct {
	Entries []*Entry `json:"entries"`
}

var OathCommand = &command.Command[OathInput, OathOutput]{
	Name:       "lore_oath",
	CLIPath:    []string{"lore", "oath"},
	CLIAliases: []string{"principles"},
	Short:      "list behavioral principles",
	Long:       "List all behavioral principles for the active project. Auto-loaded by guild_session_start — call to refresh.",
	Args: []command.ArgSpec{
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in OathInput) (OathOutput, error) {
		db, err := d.OpenDB(ctx)
		if err != nil {
			return OathOutput{}, err
		}
		defer func() { _ = db.Close() }()
		pid, err := d.ResolveProj(ctx, in.Project)
		if err != nil {
			return OathOutput{}, err
		}
		entries, err := Oath(ctx, db, pid)
		if err != nil {
			return OathOutput{}, err
		}
		return OathOutput{Entries: entries}, nil
	},
	CLIFormat: func(s command.CLISink, o OathOutput) string { return formatOath(s, o) },
	MCPFormat: func(s command.MCPSink, o OathOutput) string { return formatOath(s, o) },
}

type lineSink interface {
	Line(glyph, ascii, text string) string
	List(label string, items []string) string
}

func formatOath(s lineSink, o OathOutput) string {
	if len(o.Entries) == 0 {
		return strings.TrimRight(s.Line("⚔️", "[oath]", "no oaths sworn yet"), "\n")
	}
	var b strings.Builder
	b.WriteString(s.Line("⚔️", "[oath]", fmt.Sprintf("%d oath(s):", len(o.Entries))))
	for _, e := range o.Entries {
		fmt.Fprintf(&b, "  %s — %s\n", e.Title, e.Summary)
	}
	return strings.TrimRight(b.String(), "\n")
}
