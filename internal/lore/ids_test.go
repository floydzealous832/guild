package lore

import (
	"strings"
	"testing"

	"github.com/mathomhaus/guild/internal/command"
)

// TestParseEntryID_AcceptsAllThreeForms verifies that ParseEntryID handles
// the canonical LORE-N form, the legacy ENTRY-N form, and bare integers,
// all case-insensitively. This is the migration-safety invariant: existing
// lore summary cross-references that say "informs ENTRY-21" continue to
// resolve correctly after the display-prefix rename (QUEST-66).
func TestParseEntryID_AcceptsAllThreeForms(t *testing.T) {
	cases := []struct {
		input string
		want  int64
	}{
		// Canonical new form.
		{"LORE-23", 23},
		{"lore-23", 23},
		{"Lore-23", 23},
		// Legacy form — backward compat with stored summary text.
		{"ENTRY-23", 23},
		{"entry-23", 23},
		{"Entry-23", 23},
		// Bare integer — always accepted.
		{"23", 23},
		{"1", 1},
		{"999", 999},
		// Whitespace tolerance.
		{"  LORE-5  ", 5},
		{"  ENTRY-5  ", 5},
		{"  5  ", 5},
	}

	for _, c := range cases {
		got, err := ParseEntryID(c.input)
		if err != nil {
			t.Errorf("ParseEntryID(%q) error: %v", c.input, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseEntryID(%q) = %d; want %d", c.input, got, c.want)
		}
	}
}

// TestParseEntryID_RejectsGarbage verifies that ParseEntryID returns an
// error for inputs that cannot be resolved to a valid positive integer id.
func TestParseEntryID_RejectsGarbage(t *testing.T) {
	cases := []string{
		"",
		"foobar",
		"ENTRY-",
		"LORE-",
		"ENTRY-abc",
		"LORE-abc",
		"-1",
		"0",
		"ENTRY-0",
		"LORE-0",
		"not-a-number",
	}

	for _, input := range cases {
		_, err := ParseEntryID(input)
		if err == nil {
			t.Errorf("ParseEntryID(%q) succeeded; want error", input)
		}
	}
}

// TestFormatEntryID_UsesLorePrefix confirms the display prefix is LORE-,
// not ENTRY-.
func TestFormatEntryID_UsesLorePrefix(t *testing.T) {
	cases := []struct {
		id   int64
		want string
	}{
		{1, "LORE-1"},
		{23, "LORE-23"},
		{999, "LORE-999"},
	}
	for _, c := range cases {
		got := formatEntryID(c.id)
		if got != c.want {
			t.Errorf("formatEntryID(%d) = %q; want %q", c.id, got, c.want)
		}
	}
}

// TestDisplayPrefix_IsLore asserts the constant value so callers can
// depend on it without string literals scattered across the codebase.
func TestDisplayPrefix_IsLore(t *testing.T) {
	if DisplayPrefix != "LORE-" {
		t.Errorf("DisplayPrefix = %q; want %q", DisplayPrefix, "LORE-")
	}
}

// testSink is a minimal lineSink for testing format functions.
type testSink struct{}

func (testSink) Line(_, ascii, text string) string { return ascii + " " + text + "\n" }
func (testSink) List(label string, items []string) string {
	return label + ": " + strings.Join(items, ", ") + "\n"
}

// TestLoreInscribe_RendersLorePrefix asserts the inscribe format function
// emits LORE-N prefix and NOT ENTRY-N.
func TestLoreInscribe_RendersLorePrefix(t *testing.T) {
	s := testSink{}
	e := &Entry{ID: 5, Title: "test", Kind: KindDecision}
	out := formatInscribedBody(s, InscribeCmdOutput{Result: &InscribeResult{Entry: e}})
	if !strings.Contains(out, "LORE-5") {
		t.Errorf("formatInscribed output missing LORE-5: %q", out)
	}
	if strings.Contains(out, "ENTRY-5") {
		t.Errorf("formatInscribed output contains legacy ENTRY-5: %q", out)
	}
}

// TestLoreList_RendersLorePrefix asserts the list format function emits
// LORE-N prefix.
func TestLoreList_RendersLorePrefix(t *testing.T) {
	s := testSink{}
	entries := []*Entry{{ID: 7, Kind: KindPrinciple, Status: StatusCurrent, Title: "a principle"}}
	out := formatLoreList(s, ListCmdOutput{Entries: entries})
	if !strings.Contains(out, "LORE-7") {
		t.Errorf("formatLoreList output missing LORE-7: %q", out)
	}
	if strings.Contains(out, "ENTRY-7") {
		t.Errorf("formatLoreList output contains legacy ENTRY-7: %q", out)
	}
}

// TestLoreReforge_RendersLorePrefix asserts the reforge format function
// emits LORE-N prefix for both old and new IDs.
func TestLoreReforge_RendersLorePrefix(t *testing.T) {
	s := testSink{}
	out := formatReforged(s, ReforgeOutput{OldID: 3, NewID: 4})
	if !strings.Contains(out, "LORE-3") || !strings.Contains(out, "LORE-4") {
		t.Errorf("formatReforged output missing LORE-3 or LORE-4: %q", out)
	}
	if strings.Contains(out, "ENTRY-") {
		t.Errorf("formatReforged output contains legacy ENTRY-: %q", out)
	}
}

// TestLoreLink_RendersLorePrefix asserts the link format function emits
// LORE-N prefix for both from and to IDs.
func TestLoreLink_RendersLorePrefix(t *testing.T) {
	s := testSink{}
	out := formatLinked(s, LinkOutput{FromID: 10, ToID: 11, Relation: RelationInforms})
	if !strings.Contains(out, "LORE-10") || !strings.Contains(out, "LORE-11") {
		t.Errorf("formatLinked output missing LORE-10 or LORE-11: %q", out)
	}
	if strings.Contains(out, "ENTRY-") {
		t.Errorf("formatLinked output contains legacy ENTRY-: %q", out)
	}
}

// TestLoreSeal_RendersLorePrefix asserts the seal format function emits
// LORE-N prefix.
func TestLoreSeal_RendersLorePrefix(t *testing.T) {
	s := testSink{}
	out := formatSealed(s, SealOutput{Entry: &Entry{ID: 99}})
	if !strings.Contains(out, "LORE-99") {
		t.Errorf("formatSealed output missing LORE-99: %q", out)
	}
	if strings.Contains(out, "ENTRY-99") {
		t.Errorf("formatSealed output contains legacy ENTRY-99: %q", out)
	}
}

// TestLoreUpdate_RendersLorePrefix asserts the update format function emits
// LORE-N prefix.
func TestLoreUpdate_RendersLorePrefix(t *testing.T) {
	s := command.CLISink{NoEmoji: true}
	out := UpdateCommand.CLIFormat(s, UpdateCmdOutput{Entry: &Entry{ID: 42}})
	if !strings.Contains(out, "LORE-42") {
		t.Errorf("update CLIFormat output missing LORE-42: %q", out)
	}
	if strings.Contains(out, "ENTRY-42") {
		t.Errorf("update CLIFormat output contains legacy ENTRY-42: %q", out)
	}
}

// TestLoreAppraise_RendersLorePrefix asserts the appraise format function
// emits LORE-N prefix in its result rows.
func TestLoreAppraise_RendersLorePrefix(t *testing.T) {
	s := testSink{}
	e := &Entry{ID: 12, ProjectID: "guild", Kind: KindDecision, Status: StatusCurrent, Title: "some entry"}
	out := formatAppraiseResult(s, AppraiseCmdOutput{
		Query:  "test",
		Output: &AppraiseOutput{Results: []AppraiseResult{{Entry: e, Score: 1.0}}},
	})
	if !strings.Contains(out, "LORE-12") {
		t.Errorf("formatAppraiseResult output missing LORE-12: %q", out)
	}
	if strings.Contains(out, "ENTRY-12") {
		t.Errorf("formatAppraiseResult output contains legacy ENTRY-12: %q", out)
	}
}

// TestLoreEchoes_RendersLorePrefix asserts the echoes format function emits
// LORE-N prefix.
func TestLoreEchoes_RendersLorePrefix(t *testing.T) {
	s := testSink{}
	out := formatEchoes(s, EchoesOutput{Echoes: []Echo{{
		Entry:  &Entry{ID: 55, Kind: KindResearch},
		Reason: "expired",
	}}})
	if !strings.Contains(out, "LORE-55") {
		t.Errorf("formatEchoes output missing LORE-55: %q", out)
	}
	if strings.Contains(out, "ENTRY-55") {
		t.Errorf("formatEchoes output contains legacy ENTRY-55: %q", out)
	}
}

// TestLoreWhispers_RendersLorePrefix asserts the whispers format function
// emits LORE-N prefix.
func TestLoreWhispers_RendersLorePrefix(t *testing.T) {
	s := testSink{}
	out := formatWhispers(s, WhispersOutput{Entries: []*Entry{{ID: 77, Status: StatusSeed, Title: "new idea"}}})
	if !strings.Contains(out, "LORE-77") {
		t.Errorf("formatWhispers output missing LORE-77: %q", out)
	}
	if strings.Contains(out, "ENTRY-77") {
		t.Errorf("formatWhispers output contains legacy ENTRY-77: %q", out)
	}
}

// TestLoreInquest_RendersLorePrefix asserts the inquest format function
// emits LORE-N prefix for bloat entries.
func TestLoreInquest_RendersLorePrefix(t *testing.T) {
	s := testSink{}
	out := formatInquest(s, InquestCmdOutput{Result: &InquestResult{
		BloatEntries: []InquestRow{{EntryID: 33, WordCount: 80, Title: "a long principle"}},
	}})
	if !strings.Contains(out, "LORE-33") {
		t.Errorf("formatInquest output missing LORE-33: %q", out)
	}
	if strings.Contains(out, "ENTRY-33") {
		t.Errorf("formatInquest output contains legacy ENTRY-33: %q", out)
	}
}

// TestLoreMeld_RendersLorePrefix asserts the meld format function emits
// LORE-N prefix for duplicate pairs.
func TestLoreMeld_RendersLorePrefix(t *testing.T) {
	s := testSink{}
	out := formatMeld(s, MeldCmdOutput{Pairs: []MeldPair{{
		LeftID: 20, LeftProject: "alpha", RightID: 21, RightProject: "beta", Score: 1.0,
	}}})
	if !strings.Contains(out, "LORE-20") || !strings.Contains(out, "LORE-21") {
		t.Errorf("formatMeld output missing LORE-20 or LORE-21: %q", out)
	}
	if strings.Contains(out, "ENTRY-") {
		t.Errorf("formatMeld output contains legacy ENTRY-: %q", out)
	}
}

// TestParseEntryID_InputSiteEquivalence verifies that all three input
// forms — "LORE-23", "ENTRY-23", "23" — resolve to the same integer,
// fulfilling the dual-accept invariant for every input site.
func TestParseEntryID_InputSiteEquivalence(t *testing.T) {
	forms := []string{"LORE-23", "ENTRY-23", "23"}
	ids := make([]int64, len(forms))
	for i, f := range forms {
		id, err := ParseEntryID(f)
		if err != nil {
			t.Fatalf("ParseEntryID(%q): %v", f, err)
		}
		ids[i] = id
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] != ids[0] {
			t.Errorf("form %q resolved to %d, want %d (same as %q)",
				forms[i], ids[i], ids[0], forms[0])
		}
	}
}
