package lore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// RipplesDirection is the traversal direction enum.
type RipplesDirection string

const (
	DirOut  RipplesDirection = "out"
	DirIn   RipplesDirection = "in"
	DirBoth RipplesDirection = "both"
)

// RipplesParams is the typed input to Ripples.
type RipplesParams struct {
	SeedID    int64
	Depth     int
	Direction RipplesDirection
	Relation  string // "all" or one of the Relation constants
}

// RippleNode is one entry reached during the walk.
type RippleNode struct {
	Entry     *Entry
	Distance  int
	ViaFrom   int64
	ViaTo     int64
	ViaRel    Relation
	Direction RipplesDirection // "in" or "out" — which half of a both-walk
}

// RipplesResult is the full walk result returned by Ripples.
type RipplesResult struct {
	Seed           *Entry
	Walk           RipplesParams
	Nodes          []RippleNode
	RelationsSeen  []string
	CyclesDetected int
}

// ErrDepthExceeded is returned when depth > 10.
var ErrDepthExceeded = errors.New("lore_ripples: depth exceeds max 10")

// MaxRippleDepth is the hard cap on traversal depth.
const MaxRippleDepth = 10

// Ripples walks the provenance graph outward/inward from the seed entry
// using a SQLite recursive CTE. Direction=out follows from_id=current
// (descendants), direction=in follows to_id=current (ancestors),
// direction=both unions both. Cycle safety is handled in the CTE via a
// path-string INSTR check; the cycles_detected counter records skips.
func Ripples(ctx context.Context, db *sql.DB, params RipplesParams) (*RipplesResult, error) {
	if db == nil {
		return nil, fmt.Errorf("lore_ripples: nil db")
	}
	if params.Depth > MaxRippleDepth {
		return nil, fmt.Errorf("%w: %d", ErrDepthExceeded, params.Depth)
	}
	if params.Depth < 0 {
		params.Depth = 0
	}

	// Validate direction.
	switch params.Direction {
	case DirOut, DirIn, DirBoth:
	default:
		return nil, fmt.Errorf("lore_ripples: invalid direction %q; must be in|out|both", params.Direction)
	}

	// Validate relation.
	if params.Relation != "all" && params.Relation != "" {
		if !isValidRelation(Relation(params.Relation)) {
			return nil, fmt.Errorf("lore_ripples: invalid relation %q; must be informs|supersedes|contradicts|all", params.Relation)
		}
	}
	if params.Relation == "" {
		params.Relation = "all"
	}

	// Load seed entry.
	seed := &Entry{}
	row := db.QueryRowContext(ctx, `SELECT `+entryColumns+` FROM entries e WHERE e.id = ?`, params.SeedID) //nolint:sqlcheck // entryColumns is a package constant
	if err := scanEntry(row, seed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("lore_ripples: entry %s not found", formatEntryID(params.SeedID))
		}
		return nil, err
	}

	// Depth=0: just the seed, no edges.
	if params.Depth == 0 {
		return &RipplesResult{Seed: seed, Walk: params}, nil
	}

	// Run the walk for each requested direction.
	var outNodes, inNodes []rawRippleRow
	var cycles int
	var err error

	if params.Direction == DirOut || params.Direction == DirBoth {
		outNodes, cycles, err = runRippleCTE(ctx, db, params.SeedID, params.Depth, params.Relation, DirOut)
		if err != nil {
			return nil, err
		}
	}
	if params.Direction == DirIn || params.Direction == DirBoth {
		var c int
		inNodes, c, err = runRippleCTE(ctx, db, params.SeedID, params.Depth, params.Relation, DirIn)
		if err != nil {
			return nil, err
		}
		cycles += c
	}

	// Merge: for DirBoth, deduplicate by shortest distance. For single
	// directions, no merging needed.
	nodes, err := buildNodes(ctx, db, params, outNodes, inNodes)
	if err != nil {
		return nil, err
	}

	// Collect unique relations seen.
	relSet := map[string]struct{}{}
	for _, n := range nodes {
		if n.ViaRel != "" {
			relSet[string(n.ViaRel)] = struct{}{}
		}
	}
	var rels []string
	for r := range relSet {
		rels = append(rels, r)
	}
	sort.Strings(rels)

	return &RipplesResult{
		Seed:           seed,
		Walk:           params,
		Nodes:          nodes,
		RelationsSeen:  rels,
		CyclesDetected: cycles,
	}, nil
}

// rawRippleRow is one row from the recursive CTE (before entry lookup).
type rawRippleRow struct {
	id       int64
	distance int
	viaFrom  sql.NullInt64
	viaTo    sql.NullInt64
	viaRel   sql.NullString
	dir      RipplesDirection
}

// runRippleCTE executes the recursive CTE for one direction (in or out).
// Returns rows (excluding the seed row at distance=0) and a cycle count.
func runRippleCTE(ctx context.Context, db *sql.DB, seedID int64, depth int, relation string, dir RipplesDirection) ([]rawRippleRow, int, error) {
	// Build the join clause depending on direction.
	// out: follow from_id = current → to_id is the child.
	// in:  follow to_id = current → from_id is the child.
	var joinClause string
	if dir == DirOut {
		joinClause = `JOIN entry_links el ON el.from_id = r.id
    JOIN entries e ON e.id = el.to_id`
	} else {
		joinClause = `JOIN entry_links el ON el.to_id = r.id
    JOIN entries e ON e.id = el.from_id`
	}

	// Build relation filter clause. Use a literal comparison when
	// filtering to one relation; skip the clause when relation=all.
	relFilter := ""
	if relation != "all" {
		relFilter = "AND el.relation = ?"
	}

	//nolint:gosec // G202: joinClause and relFilter are constants derived from an enum whitelist; no user input reaches the SQL text
	sqlText := `WITH RECURSIVE ripple(id, distance, via_from, via_to, via_relation, path) AS (
  SELECT id, 0, NULL, NULL, NULL, ',' || id || ','
    FROM entries WHERE id = ?
  UNION ALL
  SELECT e.id, r.distance + 1,
         CASE WHEN ? = 'out' THEN r.id ELSE el.from_id END,
         CASE WHEN ? = 'out' THEN el.to_id ELSE r.id END,
         el.relation,
         r.path || e.id || ','
    FROM ripple r
    ` + joinClause + `
   WHERE r.distance < ?
     AND INSTR(r.path, ',' || e.id || ',') = 0
     ` + relFilter + `
)
SELECT id, distance, via_from, via_to, via_relation FROM ripple WHERE distance > 0 ORDER BY distance, id`

	args := []any{seedID, string(dir), string(dir), depth}
	if relation != "all" {
		args = append(args, relation)
	}

	rows, err := db.QueryContext(ctx, sqlText, args...) //sqlcheck:ignore // sqlText is a constant template; user values reach SQL only via args
	if err != nil {
		return nil, 0, fmt.Errorf("lore_ripples: cte query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []rawRippleRow
	seen := map[int64]struct{}{seedID: {}}
	cycles := 0

	for rows.Next() {
		var r rawRippleRow
		r.dir = dir
		if err := rows.Scan(&r.id, &r.distance, &r.viaFrom, &r.viaTo, &r.viaRel); err != nil {
			return nil, cycles, fmt.Errorf("lore_ripples: scan: %w", err)
		}
		// The CTE's INSTR check prevents re-traversal, but count any
		// self-loop row that sneaks through as a cycle.
		if _, dup := seen[r.id]; dup {
			cycles++
			continue
		}
		seen[r.id] = struct{}{}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, cycles, fmt.Errorf("lore_ripples: iterate: %w", err)
	}
	return result, cycles, nil
}

// buildNodes resolves raw CTE rows into RippleNodes with loaded Entry
// data. For direction=both it deduplicates by (id, direction) and picks
// the shortest-distance path when the same node appears in both halves.
func buildNodes(ctx context.Context, db *sql.DB, params RipplesParams, outRows, inRows []rawRippleRow) ([]RippleNode, error) {
	type key struct {
		id  int64
		dir RipplesDirection
	}

	// Index by (id, dir); keep shortest distance per key.
	best := map[key]rawRippleRow{}
	for _, r := range outRows {
		k := key{r.id, DirOut}
		if prev, ok := best[k]; !ok || r.distance < prev.distance {
			best[k] = r
		}
	}
	for _, r := range inRows {
		k := key{r.id, DirIn}
		if prev, ok := best[k]; !ok || r.distance < prev.distance {
			best[k] = r
		}
	}

	if len(best) == 0 {
		return nil, nil
	}

	// Resolve entries in ID order for deterministic output.
	ordered := make([]rawRippleRow, 0, len(best))
	for _, r := range best {
		ordered = append(ordered, r)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].distance != ordered[j].distance {
			return ordered[i].distance < ordered[j].distance
		}
		if ordered[i].id != ordered[j].id {
			return ordered[i].id < ordered[j].id
		}
		return string(ordered[i].dir) < string(ordered[j].dir)
	})

	nodes := make([]RippleNode, 0, len(ordered))
	for _, r := range ordered {
		e := &Entry{}
		eRow := db.QueryRowContext(ctx, `SELECT `+entryColumns+` FROM entries e WHERE e.id = ?`, r.id) //nolint:sqlcheck // entryColumns is a package constant
		if err := scanEntry(eRow, e); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // orphan edge — skip
			}
			return nil, err
		}
		n := RippleNode{
			Entry:     e,
			Distance:  r.distance,
			Direction: r.dir,
		}
		if r.viaFrom.Valid {
			n.ViaFrom = r.viaFrom.Int64
		}
		if r.viaTo.Valid {
			n.ViaTo = r.viaTo.Int64
		}
		if r.viaRel.Valid {
			n.ViaRel = Relation(r.viaRel.String)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// truncateTitle trims a title to at most maxLen runes for display.
func truncateTitle(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

// RenderRipples renders the text output for a RipplesResult. Used by
// both CLIFormat and MCPFormat to avoid drift.
func RenderRipples(r *RipplesResult) string {
	var b strings.Builder

	// Header line.
	fmt.Fprintf(&b, "📜 ripples for %s · depth=%d · direction=%s · relation=%s\n",
		formatEntryID(r.Seed.ID), r.Walk.Depth, r.Walk.Direction, r.Walk.Relation)

	// Split nodes by direction.
	var outNodes, inNodes []RippleNode
	for _, n := range r.Nodes {
		if n.Direction == DirIn {
			inNodes = append(inNodes, n)
		} else {
			outNodes = append(outNodes, n)
		}
	}

	showBoth := r.Walk.Direction == DirBoth
	showIn := r.Walk.Direction == DirIn || showBoth
	showOut := r.Walk.Direction == DirOut || showBoth

	if showIn {
		fmt.Fprintf(&b, "\n↑ ancestors (what informs / what superseded this):\n")
		if len(inNodes) == 0 {
			fmt.Fprintf(&b, "  (no ripples)\n")
		} else {
			renderNodeList(&b, inNodes, "in")
		}
	}

	if showOut {
		fmt.Fprintf(&b, "\n↓ descendants (what this informs / what this superseded):\n")
		if len(outNodes) == 0 {
			fmt.Fprintf(&b, "  (no ripples)\n")
		} else {
			renderNodeList(&b, outNodes, "out")
		}
	}

	// Footer.
	total := len(r.Nodes)
	relStr := "none"
	if len(r.RelationsSeen) > 0 {
		relStr = strings.Join(r.RelationsSeen, ", ")
	}
	cycleStr := ""
	if r.CyclesDetected == 1 {
		cycleStr = " · 1 cycle detected"
	} else if r.CyclesDetected > 1 {
		cycleStr = fmt.Sprintf(" · %d cycles detected", r.CyclesDetected)
	}
	fmt.Fprintf(&b, "\n%d entries walked · %d relation(s) (%s)%s",
		total, len(r.RelationsSeen), relStr, cycleStr)

	return strings.TrimRight(b.String(), "\n")
}

// renderNodeList writes the indented node lines for one direction section.
func renderNodeList(b *strings.Builder, nodes []RippleNode, dir string) {
	for _, n := range nodes {
		indent := strings.Repeat("  ", n.Distance)
		arrow := "→"
		if dir == "in" {
			arrow = "←"
		}
		rel := string(n.ViaRel)
		if rel == "" {
			rel = "?"
		}
		proj := n.Entry.ProjectID
		title := truncateTitle(n.Entry.Title, 80)
		hopWord := "hop"
		if n.Distance != 1 {
			hopWord = "hops"
		}
		fmt.Fprintf(b, "%s[%s %s %s · %d %s · %s] %s\n",
			indent, rel, arrow, formatEntryID(n.Entry.ID), n.Distance, hopWord, proj, title)
	}
}
