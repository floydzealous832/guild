package lore

import (
	"context"
	"errors"
	"testing"
)

func TestStudy_ReturnsEntryAndLinks(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	ids := seedCorpus(t, ctx, db, []fixtureEntry{
		{"p", "research", "root entry", "R", "r"},
		{"p", "decision", "target 1", "T1", "t"},
		{"p", "decision", "target 2", "T2", "t"},
	})
	rootID := ids[0]

	// root → target1 (informs, outgoing)
	// target2 → root (informs, incoming)
	_, err := db.ExecContext(ctx,
		`INSERT INTO entry_links (from_id, to_id, relation) VALUES (?, ?, 'informs')`, rootID, ids[1])
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO entry_links (from_id, to_id, relation) VALUES (?, ?, 'informs')`, ids[2], rootID)
	if err != nil {
		t.Fatal(err)
	}

	res, err := Study(ctx, db, rootID)
	if err != nil {
		t.Fatalf("Study: %v", err)
	}
	if res.Entry == nil || res.Entry.ID != rootID {
		t.Fatalf("Study returned wrong root entry: %+v", res.Entry)
	}
	if len(res.Linked) != 2 {
		t.Fatalf("Study returned %d links, want 2", len(res.Linked))
	}

	// Direction split
	var gotOutgoing, gotIncoming bool
	for _, l := range res.Linked {
		switch l.Direction {
		case EdgeOutgoing:
			gotOutgoing = true
			if l.Entry.ID != ids[1] {
				t.Errorf("outgoing edge to wrong entry: %d", l.Entry.ID)
			}
		case EdgeIncoming:
			gotIncoming = true
			if l.Entry.ID != ids[2] {
				t.Errorf("incoming edge from wrong entry: %d", l.Entry.ID)
			}
		}
	}
	if !(gotOutgoing && gotIncoming) {
		t.Fatalf("missing direction: out=%v in=%v", gotOutgoing, gotIncoming)
	}
}

func TestStudy_NotFound(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := Study(ctx, db, 9999)
	if !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("Study(missing): err=%v, want ErrEntryNotFound", err)
	}
}

func TestStudy_BumpsAccessCounter(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	ids := seedCorpus(t, ctx, db, []fixtureEntry{
		{"p", "research", "counter target", "C", "t"},
	})

	if _, err := Study(ctx, db, ids[0]); err != nil {
		t.Fatalf("Study: %v", err)
	}
	row := db.QueryRowContext(ctx, `SELECT access_count FROM entries WHERE id = ?`, ids[0])
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("access_count = %d, want 1", n)
	}
}

func TestStudy_TopLinkedPrefersOutgoingSupersedes(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()

	ids := seedCorpus(t, ctx, db, []fixtureEntry{
		{"p", "research", "root", "R", "t"},
		{"p", "research", "informs target", "I", "t"},
		{"p", "research", "supersedes target", "S", "t"},
		{"p", "research", "incoming source", "X", "t"},
	})
	root := ids[0]

	_, _ = db.ExecContext(ctx,
		`INSERT INTO entry_links (from_id, to_id, relation) VALUES (?, ?, 'informs')`, root, ids[1])
	_, _ = db.ExecContext(ctx,
		`INSERT INTO entry_links (from_id, to_id, relation) VALUES (?, ?, 'supersedes')`, root, ids[2])
	_, _ = db.ExecContext(ctx,
		`INSERT INTO entry_links (from_id, to_id, relation) VALUES (?, ?, 'informs')`, ids[3], root)

	res, err := Study(ctx, db, root)
	if err != nil {
		t.Fatalf("Study: %v", err)
	}
	if res.TopLinked == nil {
		t.Fatalf("TopLinked is nil, want outgoing supersedes edge")
	}
	if res.TopLinked.Direction != EdgeOutgoing {
		t.Errorf("TopLinked.Direction = %v, want outgoing", res.TopLinked.Direction)
	}
	if res.TopLinked.Relation != RelationSupersedes {
		t.Errorf("TopLinked.Relation = %v, want supersedes", res.TopLinked.Relation)
	}
	if res.TopLinked.Entry.ID != ids[2] {
		t.Errorf("TopLinked.Entry.ID = %d, want %d", res.TopLinked.Entry.ID, ids[2])
	}
}

func TestStudy_NoLinksNoTopLinked(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	ids := seedCorpus(t, ctx, db, []fixtureEntry{
		{"p", "research", "lonely entry", "L", "t"},
	})
	res, err := Study(ctx, db, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if res.TopLinked != nil {
		t.Fatalf("TopLinked should be nil, got %+v", res.TopLinked)
	}
}
