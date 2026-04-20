package mcp

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mathomhaus/guild/internal/quest"
)

// TestMCPTelemetry_EmitsRowPerCall verifies that calling a registry-backed
// MCP tool writes one usage.log row per invocation.
//
// Setup: isolate $HOME, write a config.toml enabling telemetry, register a
// project, bootstrap via guild_session_start, post a quest, then call
// quest_accept. Assert that ~/.guild/usage.log contains a row whose
// subcommand field matches the tool's wire name ("quest_accept").
func TestMCPTelemetry_EmitsRowPerCall(t *testing.T) {
	// isolateProject calls isolateHome internally, setting $HOME. We
	// capture the resulting $HOME via os.UserHomeDir after the call so we
	// can write the config.toml to the correct temp dir.
	isolateProject(t) // sets $HOME, registers "testproj", activates it

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	// Enable usage logging: write ~/.guild/config.toml with usage_log=true.
	// config.Load(nil) reads this via the user-wide file layer.
	guildDir := filepath.Join(home, ".guild")
	cfgPath := filepath.Join(guildDir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[telemetry]\nusage_log = true\n"), 0o600); err != nil { //nolint:gosec // test fixture; 0600 is appropriate for user config
		t.Fatalf("write config.toml: %v", err)
	}

	ctx := context.Background()

	// Post a quest so quest_accept has something to claim.
	db, err := openQuestDB(ctx)
	if err != nil {
		t.Fatalf("open quest db: %v", err)
	}
	q, err := quest.Post(ctx, db, "testproj", quest.PostParams{Subject: "telemetry smoke"})
	_ = db.Close()
	if err != nil {
		t.Fatalf("quest.Post: %v", err)
	}

	s, err := build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	_, client, cleanup := connectInMemory(t, s)
	defer cleanup()

	// Bootstrap.
	if _, err := client.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "guild_session_start",
		Arguments: map[string]any{"project": "testproj"},
	}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// Call the tool under test.
	res, err := client.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "quest_accept",
		Arguments: map[string]any{"quest_id": q.ID, "project": "testproj"},
	})
	if err != nil {
		t.Fatalf("quest_accept: %v", err)
	}
	if res.IsError {
		t.Fatalf("quest_accept IsError=true: %s", textOf(res.Content))
	}

	// Read usage.log and assert at least one row has "quest_accept" as
	// the subcommand (third TSV field: timestamp, project, subcommand, ...).
	logPath := filepath.Join(home, ".guild", "usage.log")
	data, err := os.ReadFile(logPath) //nolint:gosec // test path from t.TempDir
	if err != nil {
		t.Fatalf("read usage.log at %s: %v", logPath, err)
	}

	found := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		// TSV layout: timestamp \t project \t subcommand \t exit_code \t duration_ms
		if len(fields) >= 3 && fields[2] == "quest_accept" {
			found = true
			t.Logf("usage.log row: %s", scanner.Text())
			break
		}
	}
	if !found {
		t.Errorf("usage.log missing quest_accept row; contents:\n%s", string(data))
	}
}
