package quest

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/mathomhaus/guild/internal/command"
)

func TestSetEpic_Applies(t *testing.T) {
	db, pid := newTestDB(t)
	q1 := mustPost(t, db, pid, PostParams{Subject: "A"})
	q2 := mustPost(t, db, pid, PostParams{Subject: "B"})
	q3 := mustPost(t, db, pid, PostParams{Subject: "C"})

	result, err := SetEpic(context.Background(), db, pid, "epic-v1",
		[]string{q1.ID, q2.ID, q3.ID})
	if err != nil {
		t.Fatalf("SetEpic: %v", err)
	}
	if len(result.Applied) != 3 {
		t.Errorf("applied = %v, want 3", result.Applied)
	}
	for _, id := range []string{q1.ID, q2.ID, q3.ID} {
		got := mustLoad(t, db, pid, id)
		if got.Epic != "epic-v1" {
			t.Errorf("%s epic = %q, want epic-v1", id, got.Epic)
		}
	}
}

func TestSetEpic_SkipsMissing(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "A"})
	result, err := SetEpic(context.Background(), db, pid, "oss",
		[]string{q.ID, "QUEST-999"})
	if err != nil {
		t.Fatalf("SetEpic: %v", err)
	}
	if len(result.Applied) != 1 || result.Applied[0] != q.ID {
		t.Errorf("applied = %v, want [%s]", result.Applied, q.ID)
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != "QUEST-999" {
		t.Errorf("skipped = %v, want [QUEST-999]", result.Skipped)
	}
}

func TestSetEpic_OverwritesExistingEpic(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "s", Epic: "old-epic"})
	if _, err := SetEpic(context.Background(), db, pid, "new-epic", []string{q.ID}); err != nil {
		t.Fatalf("SetEpic: %v", err)
	}
	got := mustLoad(t, db, pid, q.ID)
	if got.Epic != "new-epic" {
		t.Errorf("epic = %q, want new-epic", got.Epic)
	}
}

// TestCampaignVocab_PostEpicAlias verifies that posting with --epic sets the
// campaign/epic field on the stored quest (backward-compat alias path).
func TestCampaignVocab_PostEpicAlias(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "s", Epic: "v1-launch"})
	got := mustLoad(t, db, pid, q.ID)
	if got.Epic != "v1-launch" {
		t.Errorf("post --epic: Epic=%q, want v1-launch", got.Epic)
	}
}

// TestCampaignVocab_ListFilterByCampaign verifies that the ListInput.Campaign
// field filters quests the same way ListInput.Epic did.
func TestCampaignVocab_ListFilterByCampaign(t *testing.T) {
	db, pid := newTestDB(t)
	mustPost(t, db, pid, PostParams{Subject: "a", Epic: "alpha"})
	mustPost(t, db, pid, PostParams{Subject: "b", Epic: "beta"})

	qs, err := List(context.Background(), db, pid, ListFilters{Epic: "alpha"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(qs) != 1 || qs[0].Epic != "alpha" {
		t.Errorf("filter by campaign: got %d quests, want 1 with epic=alpha", len(qs))
	}
}

// TestCampaignVocab_EpicHandlerViaCampaignField verifies the EpicCommand
// handler resolves Campaign (primary) over Epic (alias) when both are set.
func TestCampaignVocab_EpicHandlerViaCampaignField(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "s"})

	// The handler opens+closes its own db connection, so we verify
	// the result via the EpicResult rather than reloading from db.
	deps := command.Deps{
		OpenDB:      func(_ context.Context) (*sql.DB, error) { return db, nil },
		ResolveProj: func(_ context.Context, _ string) (string, error) { return pid, nil },
	}
	// Campaign takes precedence over Epic when both are set.
	out, err := EpicCommand.Handler(context.Background(), deps, EpicInput{
		Campaign: "campaign-name",
		Epic:     "epic-name",
		QuestIDs: []string{q.ID},
	})
	if err != nil {
		t.Fatalf("EpicCommand handler: %v", err)
	}
	if out.Result.Epic != "campaign-name" {
		t.Errorf("Campaign field should win: Result.Epic=%q, want campaign-name", out.Result.Epic)
	}
	if len(out.Result.Applied) != 1 || out.Result.Applied[0] != q.ID {
		t.Errorf("Applied=%v, want [%s]", out.Result.Applied, q.ID)
	}
}

// TestCampaignVocab_EpicHandlerViaEpicAlias verifies the EpicCommand handler
// falls back to Epic when Campaign is empty.
func TestCampaignVocab_EpicHandlerViaEpicAlias(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "s"})

	deps := command.Deps{
		OpenDB:      func(_ context.Context) (*sql.DB, error) { return db, nil },
		ResolveProj: func(_ context.Context, _ string) (string, error) { return pid, nil },
	}
	out, err := EpicCommand.Handler(context.Background(), deps, EpicInput{
		Epic:     "legacy-epic",
		QuestIDs: []string{q.ID},
	})
	if err != nil {
		t.Fatalf("EpicCommand handler via Epic alias: %v", err)
	}
	if out.Result.Epic != "legacy-epic" {
		t.Errorf("Epic alias: Result.Epic=%q, want legacy-epic", out.Result.Epic)
	}
	if len(out.Result.Applied) != 1 || out.Result.Applied[0] != q.ID {
		t.Errorf("Applied=%v, want [%s]", out.Result.Applied, q.ID)
	}
}

// TestCampaignVocab_OutputSaysCampaign verifies formatEpic uses "campaign"
// label in its ascii output rather than "epic".
func TestCampaignVocab_OutputSaysCampaign(t *testing.T) {
	out := EpicOutput{Result: &EpicResult{Epic: "v2", Applied: []string{"QUEST-1"}}}
	// Use the no-emoji CLISink so the ascii label is rendered in output.
	sink := command.CLISink{NoEmoji: true}
	formatted := formatEpic(sink, out)
	if strings.Contains(formatted, "[epic]") {
		t.Errorf("output still says [epic]: %q", formatted)
	}
	if !strings.Contains(formatted, "[campaign]") {
		t.Errorf("output does not say [campaign]: %q", formatted)
	}
}
