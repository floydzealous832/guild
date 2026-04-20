package quest

import (
	"context"
	"fmt"
	"strings"

	"github.com/mathomhaus/guild/internal/command"
)

type OrdersInput struct {
	Agent   string `json:"agent,omitempty" jsonschema:"agent id to query (defaults to 'agent')"`
	Project string `json:"project,omitempty"`
}

type OrdersOutput struct {
	Agent  string   `json:"agent"`
	Quests []*Quest `json:"quests"`
}

var OrdersCommand = &command.Command[OrdersInput, OrdersOutput]{
	Name:    "quest_orders",
	CLIPath: []string{"quest", "orders"},
	Short:   "show quests assigned to an agent",
	Long:    "List quests currently assigned to a given agent owner. Use to see a teammate's queue or your own.",
	Args: []command.ArgSpec{
		{Name: "agent", Kind: command.ArgFlag, Type: command.ArgString, Help: "agent id to query (defaults to 'agent')"},
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in OrdersInput) (OrdersOutput, error) {
		db, err := d.OpenDB(ctx)
		if err != nil {
			return OrdersOutput{}, err
		}
		defer func() { _ = db.Close() }()
		pid, err := d.ResolveProj(ctx, in.Project)
		if err != nil {
			return OrdersOutput{}, err
		}
		agent := strings.TrimSpace(in.Agent)
		qs, err := Orders(ctx, db, pid, agent)
		if err != nil {
			return OrdersOutput{}, err
		}
		reported := agent
		if reported == "" {
			reported = "agent"
		}
		return OrdersOutput{Agent: reported, Quests: qs}, nil
	},
	CLIFormat: func(s command.CLISink, o OrdersOutput) string { return formatOrders(s, o) },
	MCPFormat: func(s command.MCPSink, o OrdersOutput) string { return formatOrders(s, o) },
}

func formatOrders(s lineListSink, o OrdersOutput) string {
	if len(o.Quests) == 0 {
		return strings.TrimRight(s.Line("✅", "[ok]",
			fmt.Sprintf("no tasks assigned to '%s'", o.Agent)), "\n")
	}
	var b strings.Builder
	b.WriteString(s.Line("📋", "[orders]", fmt.Sprintf("%d task(s) for '%s':", len(o.Quests), o.Agent)))
	for _, q := range o.Quests {
		fmt.Fprintf(&b, "  %s [%s]  %s\n", q.ID, q.Priority, q.Subject)
		if q.Epic != "" {
			fmt.Fprintf(&b, "    epic: %s\n", q.Epic)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
