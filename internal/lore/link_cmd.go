package lore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mathomhaus/guild/internal/command"
)

type LinkInput struct {
	FromID   command.FlexInt64 `json:"from_id" jsonschema:"source entry id"`
	ToID     command.FlexInt64 `json:"to_id" jsonschema:"target entry id — the one being informed by from"`
	Relation string            `json:"relation,omitempty" jsonschema:"informs|supersedes|contradicts (default informs)"`
	Project  string            `json:"project,omitempty"`
}

type LinkOutput struct {
	FromID   int64    `json:"from_id"`
	ToID     int64    `json:"to_id"`
	Relation Relation `json:"relation"`
}

var LinkCommand = &command.Command[LinkInput, LinkOutput]{
	Name:    "lore_link",
	CLIPath: []string{"lore", "link"},
	Short:   "create provenance link between entries (cross-project allowed)",
	Long:    "Create an informs provenance edge between two entries. Cross-project allowed.",
	Args: []command.ArgSpec{
		{Name: "from_id", Kind: command.ArgPositional, Type: command.ArgString, Required: true, Help: "source entry id (LORE-N or bare N)"},
		{Name: "to_id", CLIFlagName: "informs", Kind: command.ArgFlag, Type: command.ArgString, Required: true, Help: "target entry id (LORE-N or bare N) — the one being informed"},
		{Name: "relation", Kind: command.ArgFlag, Type: command.ArgString, Help: "link relation: informs (default) | supersedes | contradicts"},
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in LinkInput) (LinkOutput, error) {
		fromID, toID := in.FromID.Int64(), in.ToID.Int64()
		if fromID <= 0 || toID <= 0 {
			return LinkOutput{}, errors.New("from_id and to_id required")
		}
		rel := Relation(strings.TrimSpace(in.Relation))
		if rel == "" {
			rel = RelationInforms
		}
		db, err := d.OpenDB(ctx)
		if err != nil {
			return LinkOutput{}, err
		}
		defer func() { _ = db.Close() }()
		if _, err := d.ResolveProj(ctx, in.Project); err != nil {
			return LinkOutput{}, err
		}
		if err := LinkEntries(ctx, db, fromID, toID, rel); err != nil {
			return LinkOutput{}, err
		}
		return LinkOutput{FromID: fromID, ToID: toID, Relation: rel}, nil
	},
	CLIFormat: func(s command.CLISink, o LinkOutput) string { return formatLinked(s, o) },
	MCPFormat: func(s command.MCPSink, o LinkOutput) string { return formatLinked(s, o) },
}

func formatLinked(s lineSink, o LinkOutput) string {
	msg := fmt.Sprintf("linked %s %s %s", formatEntryID(o.FromID), o.Relation, formatEntryID(o.ToID))
	return strings.TrimRight(s.Line("🔗", "[linked]", msg), "\n")
}
