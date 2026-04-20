package quest

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mathomhaus/guild/internal/command"
)

type SummonInput struct {
	QuestID string `json:"quest_id" jsonschema:"QUEST-NNN to delegate"`
	To      string `json:"to" jsonschema:"target agent id"`
	Agent   string `json:"agent,omitempty" jsonschema:"caller agent id (defaults to 'agent')"`
	Project string `json:"project,omitempty"`
}

type SummonOutput struct {
	QuestID string `json:"quest_id"`
	To      string `json:"to"`
}

var SummonCommand = &command.Command[SummonInput, SummonOutput]{
	Name:    "quest_summon",
	CLIPath: []string{"quest", "summon"},
	Short:   "delegate a quest to a teammate agent",
	Long:    "Reassign a quest to a named teammate agent. The quest's owner is updated; a [summon] event is recorded in the timeline.",
	Args: []command.ArgSpec{
		{Name: "quest_id", Kind: command.ArgPositional, Type: command.ArgString, Required: true, Help: "QUEST-NNN to delegate"},
		{Name: "to", Kind: command.ArgFlag, Type: command.ArgString, Required: true, Help: "target agent id"},
		{Name: "agent", Kind: command.ArgFlag, Type: command.ArgString, Help: "caller agent id (defaults to 'agent')", CLIOnly: true},
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in SummonInput) (SummonOutput, error) {
		if strings.TrimSpace(in.QuestID) == "" {
			return SummonOutput{}, errors.New("quest_id required")
		}
		if strings.TrimSpace(in.To) == "" {
			return SummonOutput{}, errors.New("--to (target agent) required")
		}
		db, err := d.OpenDB(ctx)
		if err != nil {
			return SummonOutput{}, err
		}
		defer func() { _ = db.Close() }()
		pid, err := d.ResolveProj(ctx, in.Project)
		if err != nil {
			return SummonOutput{}, err
		}
		caller := strings.TrimSpace(in.Agent)
		if caller == "" {
			caller = "agent"
		}
		if err := Summon(ctx, db, pid, in.QuestID, in.To, caller); err != nil {
			return SummonOutput{}, err
		}
		return SummonOutput{QuestID: strings.ToUpper(in.QuestID), To: in.To}, nil
	},
	CLIFormat:      func(s command.CLISink, o SummonOutput) string { return formatSummoned(s, o) },
	MCPFormat:      func(s command.MCPSink, o SummonOutput) string { return formatSummoned(s, o) },
	CLIErrorFormat: func(s command.CLISink, err error) (string, bool) { return formatSummonError(s, err) },
	MCPErrorFormat: func(s command.MCPSink, err error) (string, bool) { return formatSummonError(s, err) },
}

func formatSummoned(s lineListSink, o SummonOutput) string {
	msg := fmt.Sprintf("summoned %s → %s", o.To, o.QuestID)
	return strings.TrimRight(s.Line("⚔️", "[summoned]", msg), "\n")
}

func formatSummonError(s lineListSink, err error) (string, bool) {
	if errors.Is(err, ErrNotFound) {
		return strings.TrimRight(s.Line("❌", "[err]", fmt.Sprintf("quest_summon: %v", err)), "\n"), true
	}
	return "", false
}
