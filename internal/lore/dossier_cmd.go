package lore

import (
	"context"

	"github.com/mathomhaus/guild/internal/command"
)

type DossierInput struct {
	Project string `json:"project,omitempty"`
}

type DossierCmdOutput struct {
	Text string `json:"text"`
}

var DossierCommand = &command.Command[DossierInput, DossierCmdOutput]{
	Name:    "lore_dossier",
	CLIPath: []string{"lore", "dossier"},
	Short:   "emit ~2k-token project context bundle for subagents",
	Long:    "Compile ~2000-token project context for subagent spawn prompts. Use when delegating multi-file work.",
	Args: []command.ArgSpec{
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in DossierInput) (DossierCmdOutput, error) {
		db, err := d.OpenDB(ctx)
		if err != nil {
			return DossierCmdOutput{}, err
		}
		defer func() { _ = db.Close() }()
		pid, err := d.ResolveProj(ctx, in.Project)
		if err != nil {
			return DossierCmdOutput{}, err
		}
		out, err := Dossier(ctx, db, pid)
		if err != nil {
			return DossierCmdOutput{}, err
		}
		return DossierCmdOutput{Text: out.Text}, nil
	},
	// Dossier output is already a fully-formatted text blob — no sink
	// primitives needed. Both surfaces pass it through verbatim.
	CLIFormat: func(_ command.CLISink, o DossierCmdOutput) string { return o.Text },
	MCPFormat: func(_ command.MCPSink, o DossierCmdOutput) string { return o.Text },
}
