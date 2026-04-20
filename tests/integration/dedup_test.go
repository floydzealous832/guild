// dedup_test.go — cross-project dedup at inscribe time.
//
// Cross-project dedup is the default: inscribing a title that already
// exists in a sibling project surfaces a warning on stderr, with the
// source project referenced so the operator can locate the original. The
// warning is informational; the entry still persists. --strict-project
// opts back into project-scoped dedup.
package integration_test

import (
	"context"
	"path/filepath"
	"testing"
)

// TestDedup_CrossProject verifies that inscribing the same title in project B
// surfaces a "similar entries found" warning on stderr that mentions the entry
// originally inscribed in project A.
//
// Observable behavior asserted:
//   - The first inscribe (project A) succeeds silently (no dedup warn).
//   - The second inscribe (project B, same title) exits 0 (entry still persists)
//     AND stderr contains the dedup warning with a reference to the first entry.
func TestDedup_CrossProject(t *testing.T) {
	ctx := context.Background()
	homeDir := t.TempDir()

	// Two separate git repos, both registered in the same lore.db (same HOME).
	projADir := filepath.Join(homeDir, "project-alpha")
	projBDir := filepath.Join(homeDir, "project-beta")

	projA := initProject(ctx, t, homeDir, projADir)
	projB := initProject(ctx, t, homeDir, projBDir)

	t.Logf("project A: %s", projA)
	t.Logf("project B: %s", projB)

	const sharedTitle = "cross project duplicate detection regression fixture entry"

	// ── Step 1: inscribe in project A ────────────────────────────────────────
	invA := inscribe(ctx, t, homeDir, projADir,
		sharedTitle,
		"research",
		"First occurrence of this title, inscribed in project alpha for the integration test corpus.",
		"retrieval")
	assertExitOK(t, invA, "first inscribe (project A)")

	// The first inscribe should have no dedup warning on stderr.
	assertNotContains(t, invA.Stderr, "similar entries found", "first inscribe stderr")

	// Capture the ENTRY-N id from the first inscribe output for the assertion.
	t.Logf("project A inscribe stdout: %s", invA.Stdout)
	t.Logf("project A inscribe stderr: %s", invA.Stderr)

	// ── Step 2: inscribe SAME title in project B ─────────────────────────────
	invB := inscribe(ctx, t, homeDir, projBDir,
		sharedTitle,
		"research",
		"Second occurrence of the same title, inscribed in project beta for the integration test corpus.",
		"retrieval")

	// Must still exit 0 — cross-project dedup surfaces hits but does NOT block.
	assertExitOK(t, invB, "second inscribe (project B, same title)")

	t.Logf("project B inscribe stdout: %s", invB.Stdout)
	t.Logf("project B inscribe stderr: %s", invB.Stderr)

	// The dedup warning MUST appear on stderr.
	assertContains(t, invB.Stderr, "similar entries found",
		"cross-project dedup warning on stderr for project B inscribe")

	// The warning should reference project A by name (project ID) so the
	// operator can identify the source of the duplicate.
	assertContains(t, invB.Stderr, projA,
		"dedup warning should include project A name in stderr")

	// The second entry must still be persisted (stdout has the new inscribed line).
	assertContains(t, invB.Stdout, "inscribed", "second inscribe stdout must confirm insert")
}

// TestDedup_StrictProject verifies the --strict-project flag suppresses
// cross-project dedup, showing the opt-out path works.
func TestDedup_StrictProject(t *testing.T) {
	ctx := context.Background()
	homeDir := t.TempDir()

	projADir := filepath.Join(homeDir, "strict-alpha")
	projBDir := filepath.Join(homeDir, "strict-beta")

	_ = initProject(ctx, t, homeDir, projADir)
	_ = initProject(ctx, t, homeDir, projBDir)

	const sharedTitle = "strict project mode suppresses cross project dedup warning"

	// Inscribe in project A first.
	invA := inscribe(ctx, t, homeDir, projADir,
		sharedTitle,
		"observation",
		"Testing strict-project mode: dedup should be scoped to the current project only.",
		"test")
	assertExitOK(t, invA, "inscribe in strict-alpha")

	// Inscribe the same title in project B with --strict-project.
	invB := runArgs(ctx, t, homeDir, projBDir, []string{
		"lore", "inscribe", sharedTitle,
		"--kind", "observation",
		"--summary", "Same title but strict-project suppresses cross-project dedup check.",
		"--topic", "test",
		"--strict-project",
	})
	assertExitOK(t, invB, "inscribe in strict-beta with --strict-project")

	// With --strict-project, no cross-project dedup warning should appear.
	assertNotContains(t, invB.Stderr, "similar entries found",
		"--strict-project should suppress cross-project dedup warning")
}

// TestDedup_SameProject verifies that inscribing the same title within the
// same project still surfaces a dedup warning (same-project hit).
func TestDedup_SameProject(t *testing.T) {
	ctx := context.Background()
	homeDir := t.TempDir()
	projDir := filepath.Join(homeDir, "same-proj-dedup")
	_ = initProject(ctx, t, homeDir, projDir)

	const title = "same project intra-project duplicate detection scenario"

	invFirst := inscribe(ctx, t, homeDir, projDir,
		title, "research",
		"First entry in the same project for intra-project dedup test.",
		"test")
	assertExitOK(t, invFirst, "first inscribe")

	invSecond := inscribe(ctx, t, homeDir, projDir,
		title, "research",
		"Second entry in the same project with identical title for dedup test.",
		"test")
	assertExitOK(t, invSecond, "second inscribe same project")

	// Same-project hit should also appear on stderr.
	assertContains(t, invSecond.Stderr, "similar entries found",
		"same-project dedup warning expected on second inscribe")
}
