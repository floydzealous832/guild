package lore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mathomhaus/guild/internal/command"
)

type SealInput struct {
	EntryID command.FlexInt64 `json:"entry_id" jsonschema:"entry to archive"`
	Project string            `json:"project,omitempty"`
}

type SealOutput struct {
	Entry *Entry `json:"entry"`
}

var SealCommand = &command.Command[SealInput, SealOutput]{
	Name:    "lore_seal",
	CLIPath: []string{"lore", "seal"},
	Short:   "seal an entry, archiving it from active circulation",
	Long:    "Seal (archive) an entry from active circulation. History is preserved.",
	Args: []command.ArgSpec{
		{Name: "entry_id", Kind: command.ArgPositional, Type: command.ArgString, Required: true, Help: "entry id to archive (ENTRY-N or bare N)"},
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in SealInput) (SealOutput, error) {
		if in.EntryID.Int64() <= 0 {
			return SealOutput{}, errors.New("entry_id required")
		}
		db, err := d.OpenDB(ctx)
		if err != nil {
			return SealOutput{}, err
		}
		defer func() { _ = db.Close() }()
		pid, err := d.ResolveProj(ctx, in.Project)
		if err != nil {
			return SealOutput{}, err
		}
		e, err := Seal(ctx, db, in.EntryID.Int64(), pid, time.Time{})
		if err != nil {
			return SealOutput{}, err
		}
		return SealOutput{Entry: e}, nil
	},
	CLIFormat: func(s command.CLISink, o SealOutput) string { return formatSealed(s, o) },
	MCPFormat: func(s command.MCPSink, o SealOutput) string { return formatSealed(s, o) },
}

func formatSealed(s lineSink, o SealOutput) string {
	msg := fmt.Sprintf("sealed %s", formatEntryID(o.Entry.ID))
	return strings.TrimRight(s.Line("📦", "[sealed]", msg), "\n")
}
