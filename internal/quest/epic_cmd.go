package quest

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mathomhaus/guild/internal/command"
)

type EpicInput struct {
	// Campaign is the primary MCP field; Epic is the backward-compat alias.
	// At the boundary, Campaign takes precedence over Epic when both are set.
	Campaign string   `json:"campaign,omitempty" jsonschema:"campaign name to apply"`
	Epic     string   `json:"epic,omitempty" jsonschema:"campaign name (alias for campaign)"`
	QuestIDs []string `json:"quest_ids" jsonschema:"quest ids to group under this campaign"`
	Project  string   `json:"project,omitempty"`
}

type EpicOutput struct {
	Result *EpicResult `json:"result"`
}

// EpicCommand is registered as `quest campaign` (CLIPath) with `quest epic` as
// an alias (CLIAliases). The MCP tool name stays quest_epic for backward compat.
var EpicCommand = &command.Command[EpicInput, EpicOutput]{
	Name:       "quest_epic",
	CLIPath:    []string{"quest", "campaign"},
	CLIAliases: []string{"epic"},
	Short:      "bulk-set campaign",
	Long:       "Set the campaign name on a batch of quests. Group related work for reporting. Also available as `quest epic` for backward compatibility.",
	Args: []command.ArgSpec{
		{Name: "campaign", Kind: command.ArgPositional, Type: command.ArgString, Help: "campaign name (also accepted via --epic alias)"},
		{
			Name:     "quest_ids",
			Kind:     command.ArgPositional,
			Type:     command.ArgStringSlice,
			Required: true,
			Variadic: true,
			Help:     "one or more QUEST-NNN ids to group under the campaign",
		},
		{Name: "epic", Kind: command.ArgFlag, Type: command.ArgString, Help: "campaign name alias (--epic works as --campaign)", MCPOnly: true},
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in EpicInput) (EpicOutput, error) {
		name := strings.TrimSpace(in.Campaign)
		if name == "" {
			name = strings.TrimSpace(in.Epic)
		}
		if name == "" {
			return EpicOutput{}, errors.New("campaign name required")
		}
		if len(in.QuestIDs) == 0 {
			return EpicOutput{}, errors.New("at least one quest_id required")
		}
		db, err := d.OpenDB(ctx)
		if err != nil {
			return EpicOutput{}, err
		}
		defer func() { _ = db.Close() }()
		pid, err := d.ResolveProj(ctx, in.Project)
		if err != nil {
			return EpicOutput{}, err
		}
		res, err := SetEpic(ctx, db, pid, name, in.QuestIDs)
		if err != nil {
			return EpicOutput{}, err
		}
		return EpicOutput{Result: res}, nil
	},
	CLIFormat: func(s command.CLISink, o EpicOutput) string { return formatEpic(s, o) },
	MCPFormat: func(s command.MCPSink, o EpicOutput) string { return formatEpic(s, o) },
}

func formatEpic(s lineListSink, o EpicOutput) string {
	r := o.Result
	var b strings.Builder
	b.WriteString(s.Line("🏷️", "[campaign]",
		fmt.Sprintf("applied '%s' to %d quest(s)", r.Epic, len(r.Applied))))
	if len(r.Applied) > 0 {
		b.WriteString(s.List("applied", r.Applied))
	}
	if len(r.Skipped) > 0 {
		b.WriteString(s.List("skipped (not found)", r.Skipped))
	}
	return strings.TrimRight(b.String(), "\n")
}
