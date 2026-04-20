package lore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestRipples_Empty: seed entry with no edges returns just the seed.
func TestRipples_Empty(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "p")
	a, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "lonely entry with no links at all", Summary: "s", Topic: "t"})

	res, err := Ripples(ctx, db, RipplesParams{SeedID: a.Entry.ID, Depth: 3, Direction: DirOut, Relation: "all"})
	if err != nil {
		t.Fatalf("ripples: %v", err)
	}
	if res.Seed.ID != a.Entry.ID {
		t.Errorf("seed id mismatch")
	}
	if len(res.Nodes) != 0 {
		t.Errorf("want 0 nodes, got %d", len(res.Nodes))
	}
	txt := RenderRipples(res)
	if !strings.Contains(txt, "(no ripples)") {
		t.Errorf("want (no ripples) in output; got:\n%s", txt)
	}
}

// TestRipples_SingleOut: A→B, seed=A direction=out returns B at distance 1.
func TestRipples_SingleOut(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "p")
	a, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "entry A is the source for single out test", Summary: "s", Topic: "t"})
	b, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "entry B is the target for single out test", Summary: "s", Topic: "t"})
	if err := LinkEntries(ctx, db, a.Entry.ID, b.Entry.ID, RelationInforms); err != nil {
		t.Fatalf("link: %v", err)
	}

	res, err := Ripples(ctx, db, RipplesParams{SeedID: a.Entry.ID, Depth: 3, Direction: DirOut, Relation: "all"})
	if err != nil {
		t.Fatalf("ripples: %v", err)
	}
	if len(res.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(res.Nodes))
	}
	if res.Nodes[0].Entry.ID != b.Entry.ID {
		t.Errorf("want node B, got %d", res.Nodes[0].Entry.ID)
	}
	if res.Nodes[0].Distance != 1 {
		t.Errorf("want distance 1, got %d", res.Nodes[0].Distance)
	}
	txt := RenderRipples(res)
	if !strings.Contains(txt, "↓ descendants") {
		t.Errorf("want descendants section; got:\n%s", txt)
	}
}

// TestRipples_SingleIn: A→B, seed=B direction=in returns A at distance 1.
func TestRipples_SingleIn(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "p")
	a, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "entry A is the ancestor for single in test", Summary: "s", Topic: "t"})
	b, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "entry B is the descendant for single in test", Summary: "s", Topic: "t"})
	if err := LinkEntries(ctx, db, a.Entry.ID, b.Entry.ID, RelationInforms); err != nil {
		t.Fatalf("link: %v", err)
	}

	res, err := Ripples(ctx, db, RipplesParams{SeedID: b.Entry.ID, Depth: 3, Direction: DirIn, Relation: "all"})
	if err != nil {
		t.Fatalf("ripples: %v", err)
	}
	if len(res.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(res.Nodes))
	}
	if res.Nodes[0].Entry.ID != a.Entry.ID {
		t.Errorf("want node A, got %d", res.Nodes[0].Entry.ID)
	}
	if res.Nodes[0].Distance != 1 {
		t.Errorf("want distance 1, got %d", res.Nodes[0].Distance)
	}
	txt := RenderRipples(res)
	if !strings.Contains(txt, "↑ ancestors") {
		t.Errorf("want ancestors section; got:\n%s", txt)
	}
}

// TestRipples_DeepChainTruncated: A→B→C→D, seed=A depth=2 returns B,C; D truncated.
func TestRipples_DeepChainTruncated(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "p")
	a, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "chain entry A for deep truncation test", Summary: "s", Topic: "t"})
	b, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "chain entry B for deep truncation test", Summary: "s", Topic: "t"})
	c, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "chain entry C for deep truncation test", Summary: "s", Topic: "t"})
	d, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "chain entry D for deep truncation test", Summary: "s", Topic: "t"})
	_ = LinkEntries(ctx, db, a.Entry.ID, b.Entry.ID, RelationInforms)
	_ = LinkEntries(ctx, db, b.Entry.ID, c.Entry.ID, RelationInforms)
	_ = LinkEntries(ctx, db, c.Entry.ID, d.Entry.ID, RelationInforms)

	res, err := Ripples(ctx, db, RipplesParams{SeedID: a.Entry.ID, Depth: 2, Direction: DirOut, Relation: "all"})
	if err != nil {
		t.Fatalf("ripples: %v", err)
	}
	ids := make(map[int64]bool)
	for _, n := range res.Nodes {
		ids[n.Entry.ID] = true
	}
	if !ids[b.Entry.ID] || !ids[c.Entry.ID] {
		t.Errorf("want B and C in nodes; got ids: %v", ids)
	}
	if ids[d.Entry.ID] {
		t.Errorf("D should be truncated at depth=2; got ids: %v", ids)
	}
}

// TestRipples_Diamond: A→B, A→C, B→D, C→D — D appears once.
func TestRipples_Diamond(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "p")
	a, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "diamond entry A as the apex", Summary: "s", Topic: "t"})
	b, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "diamond entry B left branch", Summary: "s", Topic: "t"})
	c, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "diamond entry C right branch", Summary: "s", Topic: "t"})
	d, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "diamond entry D the bottom convergence", Summary: "s", Topic: "t"})
	_ = LinkEntries(ctx, db, a.Entry.ID, b.Entry.ID, RelationInforms)
	_ = LinkEntries(ctx, db, a.Entry.ID, c.Entry.ID, RelationInforms)
	_ = LinkEntries(ctx, db, b.Entry.ID, d.Entry.ID, RelationInforms)
	_ = LinkEntries(ctx, db, c.Entry.ID, d.Entry.ID, RelationInforms)

	res, err := Ripples(ctx, db, RipplesParams{SeedID: a.Entry.ID, Depth: 3, Direction: DirOut, Relation: "all"})
	if err != nil {
		t.Fatalf("ripples: %v", err)
	}

	// D must appear exactly once.
	var dCount int
	for _, n := range res.Nodes {
		if n.Entry.ID == d.Entry.ID {
			dCount++
		}
	}
	if dCount != 1 {
		t.Errorf("want D exactly once, got %d occurrences", dCount)
	}

	txt := RenderRipples(res)
	if !strings.Contains(txt, formatEntryID(d.Entry.ID)) {
		t.Errorf("want D in text output; got:\n%s", txt)
	}
}

// TestRipples_CycleDetection: A→B→C→A cycle; seed=A depth=5, no infinite loop.
func TestRipples_CycleDetection(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "p")
	a, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "cycle node A for cycle detection test", Summary: "s", Topic: "t"})
	b, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "cycle node B for cycle detection test", Summary: "s", Topic: "t"})
	c, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "cycle node C for cycle detection test", Summary: "s", Topic: "t"})
	_ = LinkEntries(ctx, db, a.Entry.ID, b.Entry.ID, RelationInforms)
	_ = LinkEntries(ctx, db, b.Entry.ID, c.Entry.ID, RelationInforms)
	// Close the cycle: C back to A. link.go enforces no self-links but
	// allows A→B→C→A since all three are distinct IDs.
	_ = LinkEntries(ctx, db, c.Entry.ID, a.Entry.ID, RelationInforms)

	res, err := Ripples(ctx, db, RipplesParams{SeedID: a.Entry.ID, Depth: 5, Direction: DirOut, Relation: "all"})
	if err != nil {
		t.Fatalf("ripples (must not loop): %v", err)
	}

	// B and C appear; A does not reappear.
	ids := map[int64]bool{}
	for _, n := range res.Nodes {
		ids[n.Entry.ID] = true
	}
	if ids[a.Entry.ID] {
		t.Errorf("seed A should not reappear as a node")
	}
	if !ids[b.Entry.ID] || !ids[c.Entry.ID] {
		t.Errorf("B and C should appear; got ids: %v", ids)
	}

	txt := RenderRipples(res)
	// CyclesDetected may be 0 because the CTE path-check blocks re-entry at
	// the SQL level (not our post-scan dedup). Either way no infinite loop.
	_ = txt
}

// TestRipples_RelationFilter: mixed edges, filter to informs only.
func TestRipples_RelationFilter(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "p")
	a, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "relation filter source entry A", Summary: "s", Topic: "t"})
	b, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "relation filter target B via informs", Summary: "s", Topic: "t"})
	c, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindDecision, Title: "relation filter target C via supersedes", Summary: "s", Topic: "t"})
	_ = LinkEntries(ctx, db, a.Entry.ID, b.Entry.ID, RelationInforms)
	_ = LinkEntries(ctx, db, a.Entry.ID, c.Entry.ID, RelationSupersedes)

	res, err := Ripples(ctx, db, RipplesParams{SeedID: a.Entry.ID, Depth: 3, Direction: DirOut, Relation: "informs"})
	if err != nil {
		t.Fatalf("ripples: %v", err)
	}
	ids := map[int64]bool{}
	for _, n := range res.Nodes {
		ids[n.Entry.ID] = true
	}
	if !ids[b.Entry.ID] {
		t.Errorf("B (informs) should appear")
	}
	if ids[c.Entry.ID] {
		t.Errorf("C (supersedes) should be filtered out")
	}
}

// TestRipples_DirectionBoth: returns union of in and out sections.
func TestRipples_DirectionBoth(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "p")
	parent, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "both direction parent entry", Summary: "s", Topic: "t"})
	seed, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "both direction seed entry in the middle", Summary: "s", Topic: "t"})
	child, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "both direction child entry", Summary: "s", Topic: "t"})
	_ = LinkEntries(ctx, db, parent.Entry.ID, seed.Entry.ID, RelationInforms)
	_ = LinkEntries(ctx, db, seed.Entry.ID, child.Entry.ID, RelationInforms)

	res, err := Ripples(ctx, db, RipplesParams{SeedID: seed.Entry.ID, Depth: 1, Direction: DirBoth, Relation: "all"})
	if err != nil {
		t.Fatalf("ripples: %v", err)
	}
	ids := map[int64]bool{}
	for _, n := range res.Nodes {
		ids[n.Entry.ID] = true
	}
	if !ids[parent.Entry.ID] {
		t.Errorf("parent should appear in in-walk")
	}
	if !ids[child.Entry.ID] {
		t.Errorf("child should appear in out-walk")
	}

	txt := RenderRipples(res)
	if !strings.Contains(txt, "↑ ancestors") {
		t.Errorf("want ancestors section; got:\n%s", txt)
	}
	if !strings.Contains(txt, "↓ descendants") {
		t.Errorf("want descendants section; got:\n%s", txt)
	}
}

// TestRipples_CrossProject: seed in project X, linked entry in project Y.
func TestRipples_CrossProject(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "alpha", "beta")
	a, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "alpha", Kind: KindObservation, Title: "cross project source in alpha project", Summary: "s", Topic: "t"})
	b, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "beta", Kind: KindDecision, Title: "cross project target in beta project", Summary: "s", Topic: "t"})
	_ = LinkEntries(ctx, db, a.Entry.ID, b.Entry.ID, RelationInforms)

	res, err := Ripples(ctx, db, RipplesParams{SeedID: a.Entry.ID, Depth: 2, Direction: DirOut, Relation: "all"})
	if err != nil {
		t.Fatalf("ripples: %v", err)
	}
	if len(res.Nodes) != 1 || res.Nodes[0].Entry.ID != b.Entry.ID {
		t.Errorf("expected cross-project node B; got %v", res.Nodes)
	}
	if res.Nodes[0].Entry.ProjectID != "beta" {
		t.Errorf("expected beta project; got %s", res.Nodes[0].Entry.ProjectID)
	}
}

// TestRipples_SeedNotFound: entry does not exist → clean error.
func TestRipples_SeedNotFound(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "p")

	_, err := Ripples(ctx, db, RipplesParams{SeedID: 99999, Depth: 3, Direction: DirOut, Relation: "all"})
	if err == nil {
		t.Fatal("want error for missing seed")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("want 'not found' in error; got: %v", err)
	}
}

// TestRipples_DepthCapRejection: depth=11 rejected before hitting SQL.
func TestRipples_DepthCapRejection(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "p")
	a, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "depth cap test seed entry", Summary: "s", Topic: "t"})

	_, err := Ripples(ctx, db, RipplesParams{SeedID: a.Entry.ID, Depth: 11, Direction: DirOut, Relation: "all"})
	if err == nil {
		t.Fatal("want error for depth=11")
	}
	if !errors.Is(err, ErrDepthExceeded) {
		t.Errorf("want ErrDepthExceeded; got: %v", err)
	}
}

// TestRipples_InvalidDirection: bad direction string returns clean error.
func TestRipples_InvalidDirection(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "p")
	a, _ := Inscribe(ctx, db, &InscribeParams{ProjectID: "p", Kind: KindObservation, Title: "invalid direction test seed entry", Summary: "s", Topic: "t"})

	_, err := Ripples(ctx, db, RipplesParams{SeedID: a.Entry.ID, Depth: 3, Direction: "sideways", Relation: "all"})
	if err == nil {
		t.Fatal("want error for bad direction")
	}
}
