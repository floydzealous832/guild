package cli

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mathomhaus/guild/internal/lore"
	"github.com/mathomhaus/guild/internal/storage"
)

// cliSetup prepares a tempdir-backed HOME, a migrated lore.db, and
// the one-or-more registered projects a test needs. It returns the
// *sql.DB for pre-seeding entries; the CLI commands themselves use
// the same DB path via loreDBPath() because HOME is already swapped.
func cliSetup(t *testing.T, projectIDs ...string) (db *sql.DB, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	// Also clear USERPROFILE/Windows just in case.
	t.Setenv("USERPROFILE", home)
	dbPath := filepath.Join(home, ".guild", "lore.db")
	if err := mkGuildDir(home); err != nil {
		t.Fatalf("mkdir .guild: %v", err)
	}

	ctx := context.Background()
	var err error
	db, err = storage.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := storage.MigrateTo(ctx, db, "test", nil); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, pid := range projectIDs {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO projects (id, path) VALUES (?, ?)`,
			pid, "/fake/"+pid,
		); err != nil {
			t.Fatalf("register %s: %v", pid, err)
		}
	}
	return db, home
}

func mkGuildDir(home string) error {
	return os.MkdirAll(filepath.Join(home, ".guild"), 0o755)
}

// execCmd runs rootCmd with the given args, captures stdout+stderr into
// separate buffers, and returns them + any RunE error.
func execCmd(t *testing.T, args ...string) (stdout, stderr *bytes.Buffer, err error) {
	t.Helper()
	stdout = new(bytes.Buffer)
	stderr = new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
	})
	err = rootCmd.Execute()
	return
}

// TestCLI_Inscribe_Happy runs `lore inscribe --project alpha TITLE` and
// asserts the success line shape.
func TestCLI_Inscribe_Happy(t *testing.T) {
	_, _ = cliSetup(t, "alpha")
	t.Setenv("GUILD_NO_EMOJI", "1")

	out, errOut, err := execCmd(t,
		"lore", "inscribe", "happy path cli test title",
		"--project", "alpha",
		"--kind", "decision",
		"--summary", "a summary",
		"--topic", "test",
	)
	if err != nil {
		t.Fatalf("execute: %v (stderr=%q)", err, errOut.String())
	}
	want := "[inscribed] inscribed LORE-1: happy path cli test title [decision]"
	if !strings.Contains(out.String(), want) {
		t.Errorf("stdout missing %q; got %q", want, out.String())
	}
}

// TestCLI_Inscribe_DedupLine verifies the ⚠️ duplicate block fires on
// the second inscribe with an identical title in a different project.
func TestCLI_Inscribe_DedupLine(t *testing.T) {
	_, _ = cliSetup(t, "alpha", "beta")
	t.Setenv("GUILD_NO_EMOJI", "1")

	// First inscribe in alpha.
	if _, _, err := execCmd(t,
		"lore", "inscribe", "shared duplicate title across projects here",
		"--project", "alpha",
		"--kind", "research",
		"--summary", "first one",
		"--topic", "retrieval",
	); err != nil {
		t.Fatalf("first: %v", err)
	}

	// Second with same title in beta — must surface dedup.
	out, errOut, err := execCmd(t,
		"lore", "inscribe", "shared duplicate title across projects here",
		"--project", "beta",
		"--kind", "research",
		"--summary", "second one",
		"--topic", "retrieval",
	)
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if !strings.Contains(errOut.String(), "similar entries found") {
		t.Errorf("stderr missing dedup block:\n%s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "(alpha)") {
		t.Errorf("stderr missing (alpha) project tag:\n%s", errOut.String())
	}
	if !strings.Contains(out.String(), "inscribed LORE-2") {
		t.Errorf("stdout missing success line:\n%s", out.String())
	}
}

// TestCLI_Inscribe_BloatWarning asserts the 60-word warn hits stderr
// but the entry still inserts.
func TestCLI_Inscribe_BloatWarning(t *testing.T) {
	_, _ = cliSetup(t, "alpha")
	t.Setenv("GUILD_NO_EMOJI", "1")

	longSummary := "principles bloat the oath wall when their combined title and summary exceed sixty words which happens often when agents try to encode policy as prose rather than as short memorable rules that actually fit in a session start context window without burning tokens on every session start call across every single project in the workspace today"

	out, errOut, err := execCmd(t,
		"lore", "inscribe", "a very long principle title that is nine words here",
		"--project", "alpha",
		"--kind", "principle",
		"--summary", longSummary,
		"--topic", "hygiene",
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(errOut.String(), "oath hygiene") {
		t.Errorf("stderr missing hygiene warning:\n%s", errOut.String())
	}
	if !strings.Contains(out.String(), "inscribed LORE-1") {
		t.Errorf("stdout missing success line; got %q", out.String())
	}
}

// TestCLI_Reforge runs inscribe×2 + reforge and verifies the CLI
// prints the emoji line and persists the supersedes edge.
func TestCLI_Reforge(t *testing.T) {
	_, _ = cliSetup(t, "alpha")
	t.Setenv("GUILD_NO_EMOJI", "1")

	if _, _, err := execCmd(t,
		"lore", "inscribe", "first entry to be reforged",
		"--project", "alpha", "--kind", "decision",
		"--summary", "first", "--topic", "x",
	); err != nil {
		t.Fatalf("first inscribe: %v", err)
	}
	if _, _, err := execCmd(t,
		"lore", "inscribe", "second entry replacing the first",
		"--project", "alpha", "--kind", "decision",
		"--summary", "second", "--topic", "x",
	); err != nil {
		t.Fatalf("second inscribe: %v", err)
	}

	out, errOut, err := execCmd(t,
		"lore", "reforge", "ENTRY-1", "--with", "ENTRY-2",
		"--project", "alpha",
	)
	if err != nil {
		t.Fatalf("reforge: %v (stderr=%q)", err, errOut.String())
	}
	want := "[reforged] reforged LORE-1 -> LORE-2"
	if !strings.Contains(out.String(), want) {
		t.Errorf("stdout missing %q; got %q", want, out.String())
	}
}

// TestCLI_Link prints the link emoji line; cross-project stays legal.
func TestCLI_Link(t *testing.T) {
	_, _ = cliSetup(t, "alpha", "beta")
	t.Setenv("GUILD_NO_EMOJI", "1")

	if _, _, err := execCmd(t,
		"lore", "inscribe", "alpha source entry for link test",
		"--project", "alpha", "--kind", "principle",
		"--summary", "s", "--topic", "x",
	); err != nil {
		t.Fatalf("alpha inscribe: %v", err)
	}
	if _, _, err := execCmd(t,
		"lore", "inscribe", "beta target entry for cross project link",
		"--project", "beta", "--kind", "decision",
		"--summary", "s", "--topic", "x",
	); err != nil {
		t.Fatalf("beta inscribe: %v", err)
	}

	out, errOut, err := execCmd(t,
		"lore", "link", "ENTRY-1", "--informs", "ENTRY-2",
		"--project", "alpha",
	)
	if err != nil {
		t.Fatalf("link: %v (stderr=%q)", err, errOut.String())
	}
	if !strings.Contains(out.String(), "[linked] linked LORE-1 informs LORE-2") {
		t.Errorf("stdout: %q", out.String())
	}
}

// TestCLI_Seal_Then_Update_ThenInvalidStatus covers seal + update paths.
func TestCLI_Seal_Then_Update_ThenInvalidStatus(t *testing.T) {
	_, _ = cliSetup(t, "alpha")
	t.Setenv("GUILD_NO_EMOJI", "1")

	if _, _, err := execCmd(t,
		"lore", "inscribe", "entry to be sealed then updated later",
		"--project", "alpha", "--kind", "decision",
		"--summary", "s", "--topic", "x",
	); err != nil {
		t.Fatalf("inscribe: %v", err)
	}

	out, _, err := execCmd(t, "lore", "seal", "ENTRY-1", "--project", "alpha")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if !strings.Contains(out.String(), "[sealed] sealed LORE-1") {
		t.Errorf("stdout: %q", out.String())
	}

	// Update the (now archived) entry's summary — still writable.
	out, _, err = execCmd(t,
		"lore", "update", "ENTRY-1",
		"--project", "alpha",
		"--summary", "rewritten after seal",
	)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(out.String(), "[ok] updated LORE-1") {
		t.Errorf("stdout: %q", out.String())
	}
}

// TestCLI_AliasAdd runs `lore add` (alias for inscribe) and ensures the
// entry lands the same way.
func TestCLI_AliasAdd(t *testing.T) {
	_, _ = cliSetup(t, "alpha")
	t.Setenv("GUILD_NO_EMOJI", "1")

	out, errOut, err := execCmd(t,
		"lore", "add", "alias test title for backward compat",
		"--project", "alpha", "--kind", "decision",
		"--summary", "a", "--topic", "x",
	)
	if err != nil {
		t.Fatalf("alias add: %v (stderr=%q)", err, errOut.String())
	}
	if !strings.Contains(out.String(), "inscribed LORE-1") {
		t.Errorf("stdout: %q", out.String())
	}
}

// TestCLI_Init_Registers runs the init command against a stubbed git
// toplevel. Swapping the package-level gitToplevelFn lets us avoid a
// real git repo inside the test tmp.
func TestCLI_Init_Registers(t *testing.T) {
	db, _ := cliSetup(t)
	t.Setenv("GUILD_NO_EMOJI", "1")

	orig := lore.SwapGitToplevelForTest(func(_ context.Context, _ string) (string, error) {
		return "/fake/tmp/fancy-proj", nil
	})
	t.Cleanup(func() { lore.SwapGitToplevelForTest(orig) })

	out, errOut, err := execCmd(t, "lore", "init")
	if err != nil {
		t.Fatalf("init: %v (stderr=%q)", err, errOut.String())
	}
	if !strings.Contains(out.String(), `registered project "fancy-proj"`) {
		t.Errorf("stdout: %q", out.String())
	}

	// Verify the row landed.
	var path string
	err = db.QueryRowContext(context.Background(),
		`SELECT path FROM projects WHERE id = ?`, "fancy-proj",
	).Scan(&path)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if path != "/fake/tmp/fancy-proj" {
		t.Errorf("want /fake/tmp/fancy-proj, got %q", path)
	}
}
