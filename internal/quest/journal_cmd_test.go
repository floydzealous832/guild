package quest_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mathomhaus/guild/internal/quest"
)

func TestJournalCommand_CobraSurface(t *testing.T) {
	parent := &cobra.Command{Use: "quest"}
	parent.PersistentFlags().StringP("project", "p", "", "project")
	quest.JournalCommand.BindCobra(parent, fakeDeps(t))

	sub := findSubcommand(parent, "journal")
	if sub == nil {
		t.Fatal("journal subcommand not registered")
	}
	// Variadic positional: Use ends with "..." for multi-word text.
	if got, want := sub.Use, "journal QUEST_ID TEXT..."; got != want {
		t.Errorf("Use=%q want %q", got, want)
	}
	// --agent is CLIOnly — must exist on the cobra flag set.
	if sub.Flags().Lookup("agent") == nil {
		t.Error("--agent flag missing on CLI")
	}
}

func TestJournalCommand_MCPSurface(t *testing.T) {
	tool := quest.JournalCommand.BuildMCPForTest(fakeDeps(t))
	if tool.Name != "quest_journal" {
		t.Errorf("Name=%q want quest_journal", tool.Name)
	}
	buf, _ := json.Marshal(tool.InputSchema)
	schema := string(buf)
	// agent is CLIOnly — must NOT appear in MCP schema.
	if strings.Contains(schema, `"agent"`) {
		t.Errorf("MCP schema leaked CLIOnly 'agent':\n%s", schema)
	}
	for _, want := range []string{`"quest_id"`, `"text"`} {
		if !strings.Contains(schema, want) {
			t.Errorf("MCP schema missing %s", want)
		}
	}
}
