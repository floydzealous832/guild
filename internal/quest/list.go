package quest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ListFilters controls which quests List returns.
type ListFilters struct {
	// Epic filters to quests whose epic field matches (case-insensitive).
	// Empty means no filter.
	Epic string

	// Status filters to quests with exactly this status.
	// Empty means "all non-done" (the default table view).
	Status string

	// ShowBlocked, when true, includes blocked quests in the default
	// (no-Status) view. Ignored when Status is set explicitly.
	ShowBlocked bool
}

// List returns the resolved Quest slice for projectID, filtered by f.
// The returned slice is sorted by priority (P0 < P1 < P2 < blank),
// then by quest ID ascending.
//
// The Blocks field on each Quest is computed as the inverse of DependsOn:
// for every quest Q in the result, Q.Blocks contains the IDs of quests
// whose DependsOn includes Q.ID. This requires a two-pass over the list
// and does NOT query extra rows — the full set is loaded once.
//
// Callers that need --files / --deps output iterate the returned slice
// directly; the fields are always populated from the DB. The CLI layer
// decides which columns to render.
func List(ctx context.Context, db *sql.DB, projectID string, f ListFilters) ([]*Quest, error) {
	if db == nil {
		return nil, fmt.Errorf("quest: list: nil db")
	}
	if projectID = strings.TrimSpace(projectID); projectID == "" {
		return nil, fmt.Errorf("quest: list: empty project_id")
	}

	// Step 1 — fetch task_status rows matching the filter.
	var (
		rows *sql.Rows
		err  error
	)
	switch {
	case f.Status != "":
		rows, err = db.QueryContext(ctx,
			`SELECT task_id FROM task_status
			 WHERE project_id = ? AND status = ?
			 ORDER BY task_id ASC`,
			projectID, f.Status,
		)
	case f.ShowBlocked:
		rows, err = db.QueryContext(ctx,
			`SELECT task_id FROM task_status
			 WHERE project_id = ? AND status != 'done'
			 ORDER BY task_id ASC`,
			projectID,
		)
	default:
		// Default: next + in_progress only (hides done AND blocked).
		rows, err = db.QueryContext(ctx,
			`SELECT task_id FROM task_status
			 WHERE project_id = ? AND status NOT IN ('done','blocked')
			 ORDER BY task_id ASC`,
			projectID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("quest: list: query status: %w", err)
	}

	var taskIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("quest: list: scan task_id: %w", err)
		}
		taskIDs = append(taskIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("quest: list: iterate status rows: %w", err)
	}
	_ = rows.Close()

	if len(taskIDs) == 0 {
		return nil, nil
	}

	// Step 2 — resolve full Quest for each id (spec replay + status overlay).
	// We call loadSpec + overlayStatus per quest to reuse the existing
	// event-sourcing logic without duplicating it.
	var all []*Quest
	for _, id := range taskIDs {
		q, err := Load(ctx, db, projectID, id)
		if err != nil {
			// Task disappeared between query and load (rare); skip.
			continue
		}
		// Apply epic filter before adding.
		if f.Epic != "" && !strings.EqualFold(q.Epic, f.Epic) {
			continue
		}
		all = append(all, q)
	}

	// Step 3 — compute Blocks as the inverse of DependsOn.
	// Build a map: questID → index in `all` for O(1) lookup.
	idx := make(map[string]int, len(all))
	for i, q := range all {
		idx[q.ID] = i
	}
	// For each quest that declares DependsOn, record itself in the
	// depended-on quest's Blocks list.
	for _, q := range all {
		for _, dep := range q.DependsOn {
			if i, ok := idx[dep]; ok {
				all[i].Blocks = appendUnique(all[i].Blocks, q.ID)
			}
		}
	}

	// Step 4 — sort: priority then ID.
	sort.Slice(all, func(i, j int) bool {
		pi, pj := PriorityOrder(all[i].Priority), PriorityOrder(all[j].Priority)
		if pi != pj {
			return pi < pj
		}
		return all[i].ID < all[j].ID
	})

	return all, nil
}

// appendUnique appends s to xs only if s is not already present.
func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

// QuestJSON is the wire shape for `quest list --json`.
// Every field is always present (no omitempty) so callers never see a
// missing key. Slices are initialized to empty rather than nil so JSON
// encodes as [] not null.
type QuestJSON struct {
	ID        string   `json:"id"`
	Priority  string   `json:"priority"`
	Subject   string   `json:"subject"`
	Epic      string   `json:"epic"`
	Status    string   `json:"status"`
	Owner     string   `json:"owner"`
	Files     []string `json:"files"`
	DependsOn []string `json:"depends_on"`
	Blocks    []string `json:"blocks"`
}

// ToJSON converts a Quest to the wire shape. Nil slices become empty
// slices so JSON output uses [] not null.
func ToJSON(q *Quest) QuestJSON {
	files := q.Files
	if files == nil {
		files = []string{}
	}
	deps := q.DependsOn
	if deps == nil {
		deps = []string{}
	}
	blocks := q.Blocks
	if blocks == nil {
		blocks = []string{}
	}
	return QuestJSON{
		ID:        q.ID,
		Priority:  string(q.Priority),
		Subject:   q.Subject,
		Epic:      q.Epic,
		Status:    string(q.Status),
		Owner:     q.Owner,
		Files:     files,
		DependsOn: deps,
		Blocks:    blocks,
	}
}

// MarshalListJSON serializes a Quest slice to JSON.
// Returns "[]" (not null) for an empty or nil slice.
func MarshalListJSON(qs []*Quest) ([]byte, error) {
	items := make([]QuestJSON, 0, len(qs))
	for _, q := range qs {
		items = append(items, ToJSON(q))
	}
	return json.MarshalIndent(items, "", "  ")
}
