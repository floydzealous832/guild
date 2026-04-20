package lore

import (
	"context"
	"errors"
	"testing"
)

// TestLinkEntries_CrossProject creates two entries in different projects
// and links them. entry_links must accept the edge cleanly.
func TestLinkEntries_CrossProject(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha", "beta")

	a, _ := Inscribe(ctx, db, &InscribeParams{
		ProjectID: "alpha",
		Kind:      KindPrinciple,
		Title:     "alpha principle to be the source of cross-project link",
		Summary:   "s",
		Topic:     "x",
	})
	b, _ := Inscribe(ctx, db, &InscribeParams{
		ProjectID: "beta",
		Kind:      KindDecision,
		Title:     "beta decision informed by alpha principle above",
		Summary:   "s",
		Topic:     "x",
	})

	if err := LinkEntries(ctx, db, a.Entry.ID, b.Entry.ID, RelationInforms); err != nil {
		t.Fatalf("link: %v", err)
	}

	// Verify row landed.
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entry_links
		 WHERE from_id = ? AND to_id = ? AND relation = 'informs'`,
		a.Entry.ID, b.Entry.ID,
	).Scan(&n)
	if err != nil {
		t.Fatalf("count links: %v", err)
	}
	if n != 1 {
		t.Errorf("want 1 link row, got %d", n)
	}
}

// TestLinkEntries_Idempotent verifies re-linking the same pair is a
// silent no-op (INSERT OR IGNORE semantics).
func TestLinkEntries_Idempotent(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha")
	a, _ := Inscribe(ctx, db, &InscribeParams{
		ProjectID: "alpha",
		Kind:      KindPrinciple,
		Title:     "alpha principle for idempotent link test setup",
		Summary:   "s",
		Topic:     "x",
	})
	b, _ := Inscribe(ctx, db, &InscribeParams{
		ProjectID: "alpha",
		Kind:      KindDecision,
		Title:     "alpha decision for idempotent link test setup",
		Summary:   "s",
		Topic:     "x",
	})

	if err := LinkEntries(ctx, db, a.Entry.ID, b.Entry.ID, RelationInforms); err != nil {
		t.Fatalf("link 1: %v", err)
	}
	if err := LinkEntries(ctx, db, a.Entry.ID, b.Entry.ID, RelationInforms); err != nil {
		t.Fatalf("link 2 (should be no-op): %v", err)
	}

	var n int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entry_links`).Scan(&n)
	if n != 1 {
		t.Errorf("want 1 edge total, got %d", n)
	}
}

func TestLinkEntries_InvalidRelation(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha")
	a, _ := Inscribe(ctx, db, &InscribeParams{
		ProjectID: "alpha", Kind: KindIdea,
		Title:   "idea for invalid relation rejection case",
		Summary: "s", Topic: "x",
	})
	err := LinkEntries(ctx, db, a.Entry.ID, a.Entry.ID+1, Relation("loves"))
	if !errors.Is(err, ErrInvalidRelation) {
		t.Errorf("want ErrInvalidRelation, got %v", err)
	}
}

func TestLinkEntries_SelfLink(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha")
	a, _ := Inscribe(ctx, db, &InscribeParams{
		ProjectID: "alpha", Kind: KindIdea,
		Title:   "idea for self link rejection case entry",
		Summary: "s", Topic: "x",
	})
	err := LinkEntries(ctx, db, a.Entry.ID, a.Entry.ID, RelationInforms)
	if !errors.Is(err, ErrSelfLink) {
		t.Errorf("want ErrSelfLink, got %v", err)
	}
}

func TestLinkEntries_MissingFrom(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha")
	a, _ := Inscribe(ctx, db, &InscribeParams{
		ProjectID: "alpha", Kind: KindIdea,
		Title:   "idea for missing from source entry test",
		Summary: "s", Topic: "x",
	})
	err := LinkEntries(ctx, db, 9999, a.Entry.ID, RelationInforms)
	if !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("want ErrEntryNotFound, got %v", err)
	}
}
