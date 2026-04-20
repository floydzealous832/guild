// oath_audit_test.go — Behavior #4: lore inquest surfaces principles with
// >60-word bodies (oath-wall bloat candidates) so agents can demote them
// to kind=decision.
package integration_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestOathAudit_ThreeBands seeds one principle in each word-count band and
// verifies `guild lore inquest` categorizes each correctly:
//
//	≤30 words  → "short" band  (column labeled "≤30")
//	31-60 words → "medium" band (column labeled "31-60")
//	>60 words  → "bloat" band  (column labeled ">60")
func TestOathAudit_ThreeBands(t *testing.T) {
	ctx := context.Background()
	homeDir := t.TempDir()
	projDir := filepath.Join(homeDir, "audit-proj")
	projName := initProject(ctx, t, homeDir, projDir)
	t.Logf("project: %s", projName)

	// ── Short band: ≤30 words ─────────────────────────────────────────────
	// 3-word title + 7-word summary = 10 words total (well under 30).
	shortTitle := "short principle entry"
	shortSummary := "Crisp. Fits the short band. Under thirty words."
	shortWords := len(strings.Fields(shortTitle)) + len(strings.Fields(shortSummary))
	t.Logf("short-band fixture: %d words (want ≤30)", shortWords)
	if shortWords > 30 {
		t.Fatalf("short fixture has %d words, needs ≤30", shortWords)
	}

	inv := inscribe(ctx, t, homeDir, projDir, shortTitle, "principle", shortSummary, "audit-test")
	assertExitOK(t, inv, "inscribe short principle")
	t.Logf("short inscribe stdout: %s", inv.Stdout)

	// ── Medium band: 31-60 words ──────────────────────────────────────────
	// Title ~5 words + summary ~40 words = ~45 words (inside 31-60).
	mediumTitle := "medium length principle with sufficient words"
	mediumSummary := "This principle sits comfortably in the medium word-count band between thirty-one and sixty words. Agents should aim to keep principles here or shorter to minimize session-start token costs while still capturing enough context for the rule to be actionable."
	mediumWords := len(strings.Fields(mediumTitle)) + len(strings.Fields(mediumSummary))
	t.Logf("medium-band fixture: %d words (want 31-60)", mediumWords)
	if mediumWords <= 30 || mediumWords > 60 {
		t.Fatalf("medium fixture has %d words, needs 31-60", mediumWords)
	}

	inv = inscribe(ctx, t, homeDir, projDir, mediumTitle, "principle", mediumSummary, "audit-test")
	assertExitOK(t, inv, "inscribe medium principle")
	t.Logf("medium inscribe stdout: %s", inv.Stdout)

	// ── Bloat band: >60 words ─────────────────────────────────────────────
	// Title ~10 words + summary ~70 words = ~80 words (above 60).
	bloatTitle := "extremely verbose principle that really should be a decision entry instead"
	bloatSummary := "This principle is far too long and verbose. It encodes too much policy as prose rather than as a short memorable behavioral rule. When principles exceed sixty words combined title and summary they bloat the oath wall that every agent loads at every session start costing hundreds of tokens every single time and making agents slower without providing additional value over a well-crafted decision entry."
	bloatWords := len(strings.Fields(bloatTitle)) + len(strings.Fields(bloatSummary))
	t.Logf("bloat-band fixture: %d words (want >60)", bloatWords)
	if bloatWords <= 60 {
		t.Fatalf("bloat fixture has %d words, needs >60", bloatWords)
	}

	inv = inscribe(ctx, t, homeDir, projDir, bloatTitle, "principle", bloatSummary, "audit-test")
	assertExitOK(t, inv, "inscribe bloat principle")
	t.Logf("bloat inscribe stdout: %s", inv.Stdout)

	// ── Run lore inquest ──────────────────────────────────────────────────
	inv = runArgs(ctx, t, homeDir, projDir, []string{"lore", "inquest"})
	assertExitOK(t, inv, "lore inquest")

	t.Logf("lore inquest output:\n%s", inv.Stdout)

	// Only the bloat entry should surface. Short + medium stay off the list.
	assertContains(t, inv.Stdout, "bloated principle",
		"inquest output must mention bloated principles")
	assertContains(t, inv.Stdout, bloatTitle[:30],
		"inquest output must include the bloat principle title")
	assertNotContains(t, inv.Stdout, shortTitle,
		"short principle must not appear in bloat output")
	assertNotContains(t, inv.Stdout, mediumTitle,
		"medium principle must not appear in bloat output")

	t.Logf("lore inquest: bloat entry surfaced; short and medium omitted")
}

// TestOathAudit_OnlyCountsPrinciples verifies that non-principle entries (research,
// decision, observation) are NOT counted in the oath audit.
func TestOathAudit_OnlyCountsPrinciples(t *testing.T) {
	ctx := context.Background()
	homeDir := t.TempDir()
	projDir := filepath.Join(homeDir, "kinds-proj")
	_ = initProject(ctx, t, homeDir, projDir)

	// Seed a research entry (long enough to trigger bloat if miscounted).
	researchTitle := "research entry should not appear in inquest output at all"
	researchSummary := "This is a research entry with a very long summary that exceeds sixty words to confirm inquest does not count non-principle kinds toward the audit totals. Research, decision, and observation entries do not load at session start so their length is irrelevant to the oath wall token budget."
	inv := inscribe(ctx, t, homeDir, projDir, researchTitle, "research", researchSummary, "test")
	assertExitOK(t, inv, "inscribe research entry")

	// Seed one short principle.
	inv = inscribe(ctx, t, homeDir, projDir, "one short principle", "principle", "Concise rule.", "test")
	assertExitOK(t, inv, "inscribe short principle")

	// Run inquest.
	inv = runArgs(ctx, t, homeDir, projDir, []string{"lore", "inquest"})
	assertExitOK(t, inv, "lore inquest")
	t.Logf("inquest output:\n%s", inv.Stdout)

	// Only a short principle exists; the long-bodied research entry must
	// be skipped because inquest scans principles only.
	assertContains(t, inv.Stdout, "no bloated principles",
		"inquest must report healthy oath wall when only short principles exist")
	assertNotContains(t, inv.Stdout, researchTitle[:30],
		"research entry must not surface as bloat candidate")
}
