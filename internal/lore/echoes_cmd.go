package lore

import (
	"context"
	"fmt"
	"strings"

	"github.com/mathomhaus/guild/internal/command"
)

type EchoesInput struct {
	GitAware bool   `json:"git_aware,omitempty" jsonschema:"also flag entries whose file_path was modified after creation"`
	Project  string `json:"project,omitempty"`
}

type EchoesOutput struct {
	Echoes []Echo `json:"echoes"`
}

var EchoesCommand = &command.Command[EchoesInput, EchoesOutput]{
	Name:       "lore_echoes",
	CLIPath:    []string{"lore", "echoes"},
	CLIAliases: []string{"stale"},
	Short:      "surface stale entries for review",
	Long:       "List stale/decayed entries the next agent should review or reforge. Cross-checks file_path modification times when --git-aware is set.",
	Args: []command.ArgSpec{
		{Name: "git_aware", Kind: command.ArgFlag, Type: command.ArgBool, Help: "also flag entries whose file_path changed after creation"},
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in EchoesInput) (EchoesOutput, error) {
		db, err := d.OpenDB(ctx)
		if err != nil {
			return EchoesOutput{}, err
		}
		defer func() { _ = db.Close() }()
		pid, err := d.ResolveProj(ctx, in.Project)
		if err != nil {
			return EchoesOutput{}, err
		}
		echoes, err := Echoes(ctx, db, pid, in.GitAware)
		if err != nil {
			return EchoesOutput{}, err
		}
		return EchoesOutput{Echoes: echoes}, nil
	},
	CLIFormat: func(s command.CLISink, o EchoesOutput) string { return formatEchoes(s, o) },
	MCPFormat: func(s command.MCPSink, o EchoesOutput) string { return formatEchoes(s, o) },
}

func formatEchoes(s lineSink, o EchoesOutput) string {
	if len(o.Echoes) == 0 {
		return strings.TrimRight(s.Line("✅", "[ok]", "no echoes — all knowledge is current"), "\n")
	}
	var b strings.Builder
	b.WriteString(s.Line("👻", "[stale]", fmt.Sprintf("%d fading echoes:", len(o.Echoes))))
	for _, e := range o.Echoes {
		fmt.Fprintf(&b, "  %s  [%s]  %s\n", formatEntryID(e.Entry.ID), e.Entry.Kind, e.Reason)
		fmt.Fprintf(&b, "  %s\n", e.Entry.Title)
		if e.Entry.FilePath != "" {
			fmt.Fprintf(&b, "  file: %s\n", e.Entry.FilePath)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
