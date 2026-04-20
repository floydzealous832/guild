package lore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSeal_SetsArchived(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha")
	orig, _ := Inscribe(ctx, db, &InscribeParams{
		ProjectID: "alpha",
		Kind:      KindDecision,
		Title:     "seal test entry title placeholder",
		Summary:   "s",
		Topic:     "x",
	})

	got, err := Seal(ctx, db, orig.Entry.ID, "alpha", time.Time{})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if got.Status != StatusArchived {
		t.Errorf("want archived, got %s", got.Status)
	}
}

func TestSeal_EntryNotFound(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha")
	_, err := Seal(ctx, db, 123, "alpha", time.Time{})
	if !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("want ErrEntryNotFound, got %v", err)
	}
}

func TestSeal_WrongProject(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha", "beta")
	orig, _ := Inscribe(ctx, db, &InscribeParams{
		ProjectID: "alpha",
		Kind:      KindDecision,
		Title:     "seal test cross project refusal entry",
		Summary:   "s",
		Topic:     "x",
	})
	_, err := Seal(ctx, db, orig.Entry.ID, "beta", time.Time{})
	if !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("want ErrEntryNotFound for wrong project, got %v", err)
	}
}
