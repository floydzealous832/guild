package quest

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// EpicSummary holds per-status counts for one epic.
type EpicSummary struct {
	Epic       string
	Next       int
	InProgress int
	Blocked    int
	Done       int
}

// GuildSummary is the per-project summary returned by Guild, grouped by epic.
type GuildSummary struct {
	ProjectID string
	Epics     []EpicSummary
	Totals    EpicSummary // aggregate across all epics
}

// Guild returns a per-project summary of task counts grouped by epic.
// All quests are included (not just open ones) so the done count is
// accurate and the table matches the "guild overview" use case.
func Guild(ctx context.Context, db *sql.DB, projectID string) (*GuildSummary, error) {
	if db == nil {
		return nil, fmt.Errorf("quest: guild: nil db")
	}
	if projectID = strings.TrimSpace(projectID); projectID == "" {
		return nil, fmt.Errorf("quest: guild: empty project_id")
	}

	// Load all task_status rows for the project.
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status FROM task_status WHERE project_id = ?`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("quest: guild: query status: %w", err)
	}

	type statusRow struct {
		taskID string
		status string
	}
	var statusRows []statusRow
	for rows.Next() {
		var r statusRow
		if err := rows.Scan(&r.taskID, &r.status); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("quest: guild: scan status: %w", err)
		}
		statusRows = append(statusRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("quest: guild: iterate status rows: %w", err)
	}
	_ = rows.Close()

	// Aggregate by epic. For each task, load its spec to get the epic.
	epicMap := make(map[string]*EpicSummary)
	totals := EpicSummary{Epic: "TOTAL"}

	for _, sr := range statusRows {
		spec, err := loadSpec(ctx, db, projectID, sr.taskID)
		if err != nil {
			// Silently skip unreadable specs — shouldn't happen but defensive.
			continue
		}
		epic := strings.TrimSpace(spec.Epic)
		if epic == "" {
			epic = "(none)"
		}

		if _, ok := epicMap[epic]; !ok {
			epicMap[epic] = &EpicSummary{Epic: epic}
		}
		e := epicMap[epic]

		switch Status(sr.status) {
		case StatusNext:
			e.Next++
			totals.Next++
		case StatusInProgress:
			e.InProgress++
			totals.InProgress++
		case StatusBlocked:
			e.Blocked++
			totals.Blocked++
		case StatusDone:
			e.Done++
			totals.Done++
		}
	}

	// Sort epics alphabetically — stable output for tests and humans.
	epicNames := make([]string, 0, len(epicMap))
	for name := range epicMap {
		epicNames = append(epicNames, name)
	}
	sort.Strings(epicNames)

	epics := make([]EpicSummary, 0, len(epicNames))
	for _, name := range epicNames {
		epics = append(epics, *epicMap[name])
	}

	return &GuildSummary{
		ProjectID: projectID,
		Epics:     epics,
		Totals:    totals,
	}, nil
}
