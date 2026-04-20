package lore

import (
	"context"
	"errors"
	"testing"
)

// TestUpdate_SingleField verifies a single-field change lands and other
// columns are left untouched.
func TestUpdate_SingleField(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha")

	orig, err := Inscribe(ctx, db, &InscribeParams{
		ProjectID: "alpha",
		Kind:      KindDecision,
		Title:     "original decision title for update test",
		Summary:   "original summary",
		Topic:     "arch",
	})
	if err != nil {
		t.Fatalf("inscribe: %v", err)
	}

	newSummary := "rewritten summary"
	got, err := Update(ctx, db, orig.Entry.ID, &UpdateParams{
		ProjectID: "alpha",
		Summary:   &newSummary,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Summary != newSummary {
		t.Errorf("summary: want %q, got %q", newSummary, got.Summary)
	}
	if got.Title != orig.Entry.Title {
		t.Errorf("title should be unchanged; got %q", got.Title)
	}
}

// TestUpdate_KindReclassification exercises the reclassify workflow:
// demote a bloated principle to decision via `--kind decision`.
func TestUpdate_KindReclassification(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha")

	orig, err := Inscribe(ctx, db, &InscribeParams{
		ProjectID: "alpha",
		Kind:      KindPrinciple,
		Title:     "bloated principle destined to become a decision",
		Summary:   "this started as a principle but is too long; demote it.",
		Topic:     "hygiene",
		NoWarn:    true,
	})
	if err != nil {
		t.Fatalf("inscribe: %v", err)
	}
	if orig.Entry.Kind != KindPrinciple {
		t.Fatalf("precondition: want KindPrinciple")
	}

	k := KindDecision
	got, err := Update(ctx, db, orig.Entry.ID, &UpdateParams{
		ProjectID: "alpha",
		Kind:      &k,
	})
	if err != nil {
		t.Fatalf("update kind: %v", err)
	}
	if got.Kind != KindDecision {
		t.Errorf("kind reclassification failed: got %q", got.Kind)
	}
}

// TestUpdate_NoChanges returns ErrNoChanges.
func TestUpdate_NoChanges(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha")

	orig, _ := Inscribe(ctx, db, &InscribeParams{
		ProjectID: "alpha",
		Kind:      KindDecision,
		Title:     "something something title here",
		Summary:   "s",
		Topic:     "x",
	})
	_, err := Update(ctx, db, orig.Entry.ID, &UpdateParams{ProjectID: "alpha"})
	if !errors.Is(err, ErrNoChanges) {
		t.Errorf("want ErrNoChanges, got %v", err)
	}
}

// TestUpdate_InvalidStatus is rejected before any SQL.
func TestUpdate_InvalidStatus(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha")
	orig, _ := Inscribe(ctx, db, &InscribeParams{
		ProjectID: "alpha",
		Kind:      KindDecision,
		Title:     "title for invalid status test",
		Summary:   "s",
		Topic:     "x",
	})
	bogus := Status("bogus")
	_, err := Update(ctx, db, orig.Entry.ID, &UpdateParams{
		ProjectID: "alpha",
		Status:    &bogus,
	})
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("want ErrInvalidStatus, got %v", err)
	}
}

// TestUpdate_EntryNotFound signals for a missing id.
func TestUpdate_EntryNotFound(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha")
	s := "x"
	_, err := Update(ctx, db, 9999, &UpdateParams{ProjectID: "alpha", Summary: &s})
	if !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("want ErrEntryNotFound, got %v", err)
	}
}

// TestUpdate_ProjectScoped refuses to edit another project's entry.
func TestUpdate_ProjectScoped(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha", "beta")
	orig, _ := Inscribe(ctx, db, &InscribeParams{
		ProjectID: "alpha",
		Kind:      KindDecision,
		Title:     "alpha entry cross tenant edit test",
		Summary:   "s",
		Topic:     "x",
	})
	s := "cross-tenant edit should fail"
	_, err := Update(ctx, db, orig.Entry.ID, &UpdateParams{ProjectID: "beta", Summary: &s})
	if !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("want ErrEntryNotFound for cross-tenant edit, got %v", err)
	}
}
