package quest_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mathomhaus/guild/internal/quest"
)

func TestSummonCommand_CobraSurface(t *testing.T) {
	parent := &cobra.Command{Use: "quest"}
	parent.PersistentFlags().StringP("project", "p", "", "project")
	quest.SummonCommand.BindCobra(parent, fakeDeps(t))

	sub := findSubcommand(parent, "summon")
	if sub == nil {
		t.Fatal("summon subcommand not registered")
	}
	if sub.Use != "summon QUEST_ID" {
		t.Errorf("Use=%q", sub.Use)
	}
	for _, f := range []string{"to", "agent", "json"} {
		if sub.Flags().Lookup(f) == nil {
			t.Errorf("--%s flag missing", f)
		}
	}
}

func TestOrdersCommand_CobraSurface(t *testing.T) {
	parent := &cobra.Command{Use: "quest"}
	parent.PersistentFlags().StringP("project", "p", "", "project")
	quest.OrdersCommand.BindCobra(parent, fakeDeps(t))

	sub := findSubcommand(parent, "orders")
	if sub == nil {
		t.Fatal("orders subcommand not registered")
	}
	if sub.Use != "orders" {
		t.Errorf("Use=%q", sub.Use)
	}
}

func TestCampfireCommand_CobraSurface(t *testing.T) {
	parent := &cobra.Command{Use: "quest"}
	parent.PersistentFlags().StringP("project", "p", "", "project")
	quest.CampfireCommand.BindCobra(parent, fakeDeps(t))

	sub := findSubcommand(parent, "campfire")
	if sub == nil {
		t.Fatal("campfire subcommand not registered")
	}
	if sub.Use != "campfire QUEST_ID" {
		t.Errorf("Use=%q", sub.Use)
	}
	for _, f := range []string{"hypothesis", "tried", "next", "token-warning", "agent", "json"} {
		if sub.Flags().Lookup(f) == nil {
			t.Errorf("--%s flag missing", f)
		}
	}
}

func TestSimpleQuestCommands_MCPSurface(t *testing.T) {
	cases := []struct {
		toolName    string
		tool        func() string
		requiredKey string
	}{
		{"quest_summon", func() string {
			b, _ := json.Marshal(quest.SummonCommand.BuildMCPForTest(fakeDeps(t)).InputSchema)
			return string(b)
		}, "to"},
		{"quest_orders", func() string {
			b, _ := json.Marshal(quest.OrdersCommand.BuildMCPForTest(fakeDeps(t)).InputSchema)
			return string(b)
		}, "agent"},
		{"quest_campfire", func() string {
			b, _ := json.Marshal(quest.CampfireCommand.BuildMCPForTest(fakeDeps(t)).InputSchema)
			return string(b)
		}, "quest_id"},
	}
	for _, tc := range cases {
		t.Run(tc.toolName, func(t *testing.T) {
			schema := tc.tool()
			if !strings.Contains(schema, `"`+tc.requiredKey+`"`) {
				t.Errorf("schema missing %q:\n%s", tc.requiredKey, schema)
			}
		})
	}
}
