package quest_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mathomhaus/guild/internal/quest"
)

func TestForfeitCommand_CobraSurface(t *testing.T) {
	parent := &cobra.Command{Use: "quest"}
	parent.PersistentFlags().StringP("project", "p", "", "project")
	quest.ForfeitCommand.BindCobra(parent, fakeDeps(t))

	sub := findSubcommand(parent, "forfeit")
	if sub == nil {
		t.Fatal("forfeit subcommand not registered")
	}
	if got, want := sub.Use, "forfeit QUEST_ID"; got != want {
		t.Errorf("Use=%q want %q", got, want)
	}
	for _, want := range []string{"note", "json"} {
		if sub.Flags().Lookup(want) == nil {
			t.Errorf("--%s flag missing", want)
		}
	}
}

func TestForfeitCommand_MCPSurface(t *testing.T) {
	tool := quest.ForfeitCommand.BuildMCPForTest(fakeDeps(t))
	if tool.Name != "quest_forfeit" {
		t.Errorf("Name=%q want quest_forfeit", tool.Name)
	}
	if !strings.HasPrefix(tool.Description, "Release a claimed quest") {
		t.Errorf("Description=%q", tool.Description)
	}
	buf, _ := json.Marshal(tool.InputSchema)
	schema := string(buf)
	for _, want := range []string{`"quest_id"`, `"note"`, `"project"`} {
		if !strings.Contains(schema, want) {
			t.Errorf("schema missing %s:\n%s", want, schema)
		}
	}
}
