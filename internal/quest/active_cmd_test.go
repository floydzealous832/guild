package quest_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mathomhaus/guild/internal/quest"
)

func TestActiveCommand_CobraSurface(t *testing.T) {
	parent := &cobra.Command{Use: "quest"}
	parent.PersistentFlags().StringP("project", "p", "", "project")
	quest.ActiveCommand.BindCobra(parent, fakeDeps(t))

	sub := findSubcommand(parent, "active")
	if sub == nil {
		t.Fatal("active subcommand not registered")
	}
	if sub.Use != "active" {
		t.Errorf("Use=%q want 'active'", sub.Use)
	}
}

func TestActiveCommand_MCPSurface(t *testing.T) {
	tool := quest.ActiveCommand.BuildMCPForTest(fakeDeps(t))
	if tool.Name != "quest_active" {
		t.Errorf("Name=%q", tool.Name)
	}
	buf, _ := json.Marshal(tool.InputSchema)
	schema := string(buf)
	if !strings.Contains(schema, `"project"`) {
		t.Errorf("schema missing project: %s", schema)
	}
}
