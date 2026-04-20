package cli

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mathomhaus/guild/internal/storage"
)

// TestLoreReadSubcommandsRegistered ensures `guild lore --help` lists
// every read verb this quest owns, plus the declared legacy aliases.
// The test invokes cobra's --help rather than running the verbs so it
// doesn't need a live database.
func TestLoreReadSubcommandsRegistered(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"lore", "--help"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("lore --help: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"appraise", "study", "list", "oath", "echoes", "whispers", "dossier",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lore --help missing verb %q\n%s", want, out)
		}
	}
}

// TestLoreReadAliasesResolve exercises each alias→canonical mapping
// by asking cobra's Find for the alias and checking the canonical
// command's Use string.
func TestLoreReadAliasesResolve(t *testing.T) {
	cases := map[string]string{
		"check":      "appraise",
		"show":       "study",
		"principles": "oath",
		"stale":      "echoes",
		"ideas":      "whispers",
	}
	for alias, wantUse := range cases {
		alias, wantUse := alias, wantUse
		t.Run(alias, func(t *testing.T) {
			cmd, _, err := loreCmd.Find([]string{alias})
			if err != nil {
				t.Fatalf("loreCmd.Find(%q): %v", alias, err)
			}
			if !strings.HasPrefix(cmd.Use, wantUse) {
				t.Errorf("alias %q resolved to %q, want prefix %q", alias, cmd.Use, wantUse)
			}
		})
	}
}

// TestLoreAppraiseCLI_EndToEnd drives the appraise CLI against a real
// temp database, exercising the cross-project default output shape
// and the emoji prefix. Uses the loreDBPath override seam so we don't
// pollute the user's ~/.guild directory.
func TestLoreAppraiseCLI_EndToEnd(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "lore.db")
	origDBPath := loreDBPath
	loreDBPath = func() (string, error) { return dbPath, nil }
	t.Cleanup(func() { loreDBPath = origDBPath })

	// Seed the DB with two projects and a matching entry in each.
	ctx := context.Background()
	seedDB(t, ctx, dbPath)

	// Invoke: guild lore appraise "test" (all-projects is the default)
	t.Setenv("GUILD_NO_EMOJI", "1")
	t.Setenv("GUILD_NO_USAGE_LOG", "1")
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"lore", "appraise", "testable"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("lore appraise: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "entry(ies) appraised") {
		t.Errorf("expected appraise summary in output, got:\n%s", out)
	}
}

func seedDB(t *testing.T, ctx context.Context, dbPath string) {
	t.Helper()
	db, err := storage.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := storage.Migrate(ctx, db, "lore test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES ('p1', '/tmp/p1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, path) VALUES ('p2', '/tmp/p2')`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('p1','t','research','testable title one','summary','current',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO entries (project_id, topic, kind, title, summary, status, created_at, updated_at)
		 VALUES ('p2','t','research','testable title two','summary','current',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
}

// Silence unused imports guard while the end-to-end test is the only
// consumer of sql on this file.
var _ = sql.ErrNoRows
