package lore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mathomhaus/guild/internal/command"
)

// RipplesInput is the MCP/CLI input shape for lore_ripples.
type RipplesInput struct {
	EntryID   string `json:"entry_id" jsonschema:"seed entry id (LORE-N, ENTRY-N, or bare N)"`
	Depth     int    `json:"depth,omitempty" jsonschema:"walk depth, default 3, max 10"`
	Direction string `json:"direction,omitempty" jsonschema:"out|in|both — out follows descendants, in follows ancestors (default out)"`
	Relation  string `json:"relation,omitempty" jsonschema:"informs|supersedes|contradicts|all (default all)"`
	Project   string `json:"project,omitempty"`
}

// RipplesCmdOutput is the structured result returned by the handler.
type RipplesCmdOutput struct {
	Result *RipplesResult `json:"result"`
}

// RipplesCommand is the lore_ripples Command[I,O] spec.
var RipplesCommand = &command.Command[RipplesInput, RipplesCmdOutput]{
	Name:    "lore_ripples",
	CLIPath: []string{"lore", "ripples"},
	Short:   "walk provenance edges from a seed entry",
	Long:    "Walk the provenance graph from a seed entry via entry_links. direction=out follows descendants (what this informed), direction=in follows ancestors (what informed this), direction=both unions both. Default depth=3, max 10.",
	Args: []command.ArgSpec{
		{Name: "entry_id", Kind: command.ArgPositional, Type: command.ArgString, Required: true, Help: "seed entry id (LORE-N or bare N)"},
		{Name: "depth", Kind: command.ArgFlag, Type: command.ArgInt, Help: "walk depth (default 3, max 10)"},
		{Name: "direction", Kind: command.ArgFlag, Type: command.ArgString, Help: "out|in|both (default out)"},
		{Name: "relation", Kind: command.ArgFlag, Type: command.ArgString, Help: "informs|supersedes|contradicts|all (default all)"},
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in RipplesInput) (RipplesCmdOutput, error) {
		id, err := ParseEntryID(in.EntryID)
		if err != nil {
			return RipplesCmdOutput{}, fmt.Errorf("lore_ripples: %w", err)
		}

		depth := in.Depth
		if depth == 0 {
			depth = 3
		}
		if depth > MaxRippleDepth {
			return RipplesCmdOutput{}, fmt.Errorf("lore_ripples: depth %d exceeds max %d", depth, MaxRippleDepth)
		}

		dir := RipplesDirection(strings.TrimSpace(in.Direction))
		if dir == "" {
			dir = DirOut
		}
		switch dir {
		case DirOut, DirIn, DirBoth:
		default:
			return RipplesCmdOutput{}, fmt.Errorf("lore_ripples: invalid direction %q; must be in|out|both", dir)
		}

		rel := strings.TrimSpace(in.Relation)
		if rel == "" {
			rel = "all"
		}
		if rel != "all" && !isValidRelation(Relation(rel)) {
			return RipplesCmdOutput{}, fmt.Errorf("lore_ripples: invalid relation %q; must be informs|supersedes|contradicts|all", rel)
		}

		// ResolveProj for auto-bootstrap side-effect; ripples are cross-project.
		if _, err := d.ResolveProj(ctx, in.Project); err != nil {
			return RipplesCmdOutput{}, err
		}

		db, err := d.OpenDB(ctx)
		if err != nil {
			return RipplesCmdOutput{}, err
		}
		defer func() { _ = db.Close() }()

		res, err := Ripples(ctx, db, RipplesParams{
			SeedID:    id,
			Depth:     depth,
			Direction: dir,
			Relation:  rel,
		})
		if err != nil {
			return RipplesCmdOutput{}, err
		}
		return RipplesCmdOutput{Result: res}, nil
	},
	CLIFormat: func(s command.CLISink, o RipplesCmdOutput) string { return formatRipples(o) },
	MCPFormat: func(s command.MCPSink, o RipplesCmdOutput) string { return formatRipples(o) },
	CLIErrorFormat: func(s command.CLISink, err error) (string, bool) {
		if errors.Is(err, ErrDepthExceeded) || errors.Is(err, ErrEntryNotFound) {
			return strings.TrimRight(s.Line("❌", "[err]", err.Error()), "\n"), true
		}
		return "", false
	},
	MCPErrorFormat: func(s command.MCPSink, err error) (string, bool) {
		if errors.Is(err, ErrDepthExceeded) || errors.Is(err, ErrEntryNotFound) {
			return strings.TrimRight(s.Line("❌", "[err]", err.Error()), "\n"), true
		}
		return "", false
	},
}

// formatRipples delegates to the shared renderer.
func formatRipples(o RipplesCmdOutput) string {
	return RenderRipples(o.Result)
}
