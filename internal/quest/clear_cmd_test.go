package quest_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mathomhaus/guild/internal/quest"
)

func TestClearCommand_CobraSurface(t *testing.T) {
	parent := &cobra.Command{Use: "quest"}
	parent.PersistentFlags().StringP("project", "p", "", "project")
	quest.ClearCommand.BindCobra(parent, fakeDeps(t))

	sub := findSubcommand(parent, "clear")
	if sub == nil {
		t.Fatal("clear subcommand not registered")
	}
	if got, want := sub.Use, "clear QUEST_ID"; got != want {
		t.Errorf("Use=%q want %q", got, want)
	}
	if sub.Short != quest.ClearCommand.Short {
		t.Errorf("Short=%q want %q", sub.Short, quest.ClearCommand.Short)
	}
	for _, want := range []string{"report", "json"} {
		if sub.Flags().Lookup(want) == nil {
			t.Errorf("--%s flag missing", want)
		}
	}
	// project inherited, not re-registered
	if sub.LocalFlags().Lookup("project") != nil {
		t.Error("--project re-registered locally; should inherit from parent")
	}
}

func TestClearCommand_MCPSurface(t *testing.T) {
	tool := quest.ClearCommand.BuildMCPForTest(fakeDeps(t))
	if tool.Name != "quest_clear" {
		t.Errorf("Name=%q want quest_clear", tool.Name)
	}
	if !strings.HasPrefix(tool.Description, "Complete a quest") {
		t.Errorf("Description=%q", tool.Description)
	}
	buf, _ := json.Marshal(tool.InputSchema)
	schema := string(buf)
	for _, want := range []string{`"quest_id"`, `"report"`, `"project"`} {
		if !strings.Contains(schema, want) {
			t.Errorf("schema missing %s:\n%s", want, schema)
		}
	}
}

// findSubcommand returns the direct child of parent named name, or nil.
// Shared across conformance tests in this package.
func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}
