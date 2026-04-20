package quest

import (
	"context"
	"encoding/json"
	"testing"
)

func TestList_Empty(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	qs, err := List(ctx, db, pid, ListFilters{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(qs) != 0 {
		t.Fatalf("want 0 quests, got %d", len(qs))
	}
}

func TestList_BasicSort(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	mustPost(t, db, pid, PostParams{Subject: "alpha", Priority: "P2"})
	mustPost(t, db, pid, PostParams{Subject: "beta", Priority: "P0"})
	mustPost(t, db, pid, PostParams{Subject: "gamma", Priority: "P1"})

	qs, err := List(ctx, db, pid, ListFilters{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(qs) != 3 {
		t.Fatalf("want 3, got %d", len(qs))
	}
	// Should be sorted P0 < P1 < P2.
	if qs[0].Priority != "P0" || qs[1].Priority != "P1" || qs[2].Priority != "P2" {
		t.Errorf("wrong sort: got priorities %q %q %q",
			qs[0].Priority, qs[1].Priority, qs[2].Priority)
	}
}

func TestList_EpicFilter(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	mustPost(t, db, pid, PostParams{Subject: "task a", Epic: "alpha"})
	mustPost(t, db, pid, PostParams{Subject: "task b", Epic: "beta"})
	mustPost(t, db, pid, PostParams{Subject: "task c", Epic: "alpha"})

	qs, err := List(ctx, db, pid, ListFilters{Epic: "alpha"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(qs) != 2 {
		t.Fatalf("want 2, got %d", len(qs))
	}
}

func TestList_BlocksInverse(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	a := mustPost(t, db, pid, PostParams{Subject: "A"})
	b := mustPost(t, db, pid, PostParams{Subject: "B", DependsOn: []string{a.ID}})

	qs, err := List(ctx, db, pid, ListFilters{ShowBlocked: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(qs) != 2 {
		t.Fatalf("want 2, got %d", len(qs))
	}

	// Find A and B in the result.
	var qa, qb *Quest
	for _, q := range qs {
		switch q.ID {
		case a.ID:
			qa = q
		case b.ID:
			qb = q
		}
	}
	if qa == nil || qb == nil {
		t.Fatal("didn't find both quests in result")
	}
	// B.depends_on = [A]
	if len(qb.DependsOn) != 1 || qb.DependsOn[0] != a.ID {
		t.Errorf("B.DependsOn = %v, want [%s]", qb.DependsOn, a.ID)
	}
	// A.blocks = [B] (computed inverse)
	if len(qa.Blocks) != 1 || qa.Blocks[0] != b.ID {
		t.Errorf("A.Blocks = %v, want [%s]", qa.Blocks, b.ID)
	}
	// B.blocks = [] (B is not depended on by anyone)
	if len(qb.Blocks) != 0 {
		t.Errorf("B.Blocks = %v, want []", qb.Blocks)
	}
}

func TestList_JSONShape(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	a := mustPost(t, db, pid, PostParams{
		Subject:  "alpha",
		Priority: "P1",
		Epic:     "e1",
		Files:    []string{"foo.go"},
	})
	b := mustPost(t, db, pid, PostParams{
		Subject:   "beta",
		DependsOn: []string{a.ID},
	})

	qs, err := List(ctx, db, pid, ListFilters{ShowBlocked: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	jsonBytes, err := MarshalListJSON(qs)
	if err != nil {
		t.Fatalf("MarshalListJSON: %v", err)
	}

	// Parse back and verify required fields.
	var items []QuestJSON
	if err := json.Unmarshal(jsonBytes, &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}

	// Verify all required fields are present and non-null by checking
	// the raw JSON keys.
	type rawItem struct {
		ID        *string  `json:"id"`
		Priority  *string  `json:"priority"`
		Subject   *string  `json:"subject"`
		Epic      *string  `json:"epic"`
		Status    *string  `json:"status"`
		Owner     *string  `json:"owner"`
		Files     []string `json:"files"`
		DependsOn []string `json:"depends_on"`
		Blocks    []string `json:"blocks"`
	}
	var raw []rawItem
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for i, r := range raw {
		if r.ID == nil {
			t.Errorf("item[%d]: id missing", i)
		}
		if r.Priority == nil {
			t.Errorf("item[%d]: priority missing", i)
		}
		if r.Subject == nil {
			t.Errorf("item[%d]: subject missing", i)
		}
		if r.Epic == nil {
			t.Errorf("item[%d]: epic missing", i)
		}
		if r.Status == nil {
			t.Errorf("item[%d]: status missing", i)
		}
		if r.Owner == nil {
			t.Errorf("item[%d]: owner missing", i)
		}
		// files/depends_on/blocks must be arrays (not null) — verified by
		// []string scan above (nil slice = json null would cause unmarshal error
		// into []string if the field is missing — but encoding/json allows null → nil).
		// We explicitly verify that MarshalListJSON encodes them as [] not null.
		var checkNull []map[string]json.RawMessage
		if err := json.Unmarshal(jsonBytes, &checkNull); err != nil {
			t.Fatalf("unmarshal for null check: %v", err)
		}
		for _, field := range []string{"files", "depends_on", "blocks"} {
			v := checkNull[i][field]
			if string(v) == "null" || len(v) == 0 {
				t.Errorf("item[%d].%s is null/missing in JSON, want []", i, field)
			}
		}
	}

	// Verify blocks inversion: A should block B.
	_ = b // used to keep compiler happy; actual check is in the JSON.
	for _, item := range items {
		if item.ID == a.ID {
			if len(item.Blocks) != 1 || item.Blocks[0] != b.ID {
				t.Errorf("A.blocks = %v, want [%s]", item.Blocks, b.ID)
			}
		}
	}
}

func TestList_StatusFilter(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	q1 := mustPost(t, db, pid, PostParams{Subject: "task1"})
	_, err := Accept(context.Background(), db, pid, q1.ID, "agent")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	mustPost(t, db, pid, PostParams{Subject: "task2"})

	// Default list should include both next and in_progress.
	qs, err := List(ctx, db, pid, ListFilters{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(qs) != 2 {
		t.Fatalf("default: want 2, got %d", len(qs))
	}

	// Filter by in_progress.
	qs, err = List(ctx, db, pid, ListFilters{Status: "in_progress"})
	if err != nil {
		t.Fatalf("List in_progress: %v", err)
	}
	if len(qs) != 1 {
		t.Fatalf("in_progress: want 1, got %d", len(qs))
	}
	if qs[0].Status != StatusInProgress {
		t.Errorf("want in_progress, got %s", qs[0].Status)
	}
}

func TestList_FilesAndDeps(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	a := mustPost(t, db, pid, PostParams{
		Subject:   "A",
		Files:     []string{"main.go", "util.go"},
		DependsOn: nil,
	})
	mustPost(t, db, pid, PostParams{
		Subject:   "B",
		DependsOn: []string{a.ID},
	})

	qs, err := List(ctx, db, pid, ListFilters{ShowBlocked: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var qA *Quest
	for _, q := range qs {
		if q.ID == a.ID {
			qA = q
		}
	}
	if qA == nil {
		t.Fatal("A not found")
	}
	if len(qA.Files) != 2 {
		t.Errorf("A.Files = %v, want [main.go util.go]", qA.Files)
	}
}
