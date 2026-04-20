package main

import (
	"strings"
	"testing"

	"github.com/mathomhaus/guild/internal/cli"
)

// TestRenderCLI_ContainsEveryRegisteredVerb asserts that adding a new
// cobra subcommand shows up in docs/generated/cli.md automatically —
// no manual update to a separate list. Catches "added a verb, forgot
// docgen" at CI time.
func TestRenderCLI_ContainsEveryRegisteredVerb(t *testing.T) {
	doc, err := renderCLI(cli.Root())
	if err != nil {
		t.Fatalf("renderCLI: %v", err)
	}
	root := cli.Root()
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" {
			continue
		}
		if !strings.Contains(doc, "`"+c.CommandPath()+"`") {
			t.Errorf("cli.md missing verb %q", c.CommandPath())
		}
		for _, sc := range c.Commands() {
			if sc.Hidden || sc.Name() == "help" {
				continue
			}
			if !strings.Contains(doc, "`"+sc.CommandPath()+"`") {
				t.Errorf("cli.md missing verb %q", sc.CommandPath())
			}
		}
	}
}

// TestRenderMCP_ContainsEveryTool asserts that every registered MCP
// tool appears in docs/generated/mcp.md.
func TestRenderMCP_ContainsEveryTool(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	doc, err := renderMCP()
	if err != nil {
		t.Fatalf("renderMCP: %v", err)
	}
	// Minimum sanity — every major tool class should appear.
	wanted := []string{
		"guild_session_start",
		"quest_accept",
		"quest_post",
		"lore_appraise",
		"lore_inscribe",
		"lore_oath",
	}
	for _, w := range wanted {
		if !strings.Contains(doc, "`"+w+"`") {
			t.Errorf("mcp.md missing tool %q", w)
		}
	}
}

// TestRenderReadme_Banner ensures the DO-NOT-EDIT banner leads every
// generated file — humans who find cli.md should know not to hand-edit.
func TestRenderReadme_Banner(t *testing.T) {
	if !strings.HasPrefix(renderReadme(), autogenBanner) {
		t.Error("README.md missing autogen banner")
	}
}
