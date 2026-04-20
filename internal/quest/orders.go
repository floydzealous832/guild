package quest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// Orders returns all quests currently assigned (status=in_progress,
// claimed_by=agentID) in projectID.
//
// agentID resolution order:
//  1. Explicit agentID argument (non-empty)
//  2. $PM_OWNER env var
//  3. $GUILD_AGENT env var
//  4. $USER env var
//  5. "agent"
//
// All matching quests are loaded via Load so callers get fully-resolved
// Quest shapes (spec fields + status overlay).
func Orders(ctx context.Context, db *sql.DB, projectID, agentID string) ([]*Quest, error) {
	if db == nil {
		return nil, fmt.Errorf("quest: orders: nil db")
	}
	if projectID = strings.TrimSpace(projectID); projectID == "" {
		return nil, fmt.Errorf("quest: orders: empty project_id")
	}
	agentID = resolveAgent(agentID)

	rows, err := db.QueryContext(ctx,
		`SELECT task_id FROM task_status
		 WHERE project_id = ? AND claimed_by = ? AND status = 'in_progress'
		 ORDER BY task_id ASC`,
		projectID, agentID,
	)
	if err != nil {
		return nil, fmt.Errorf("quest: orders: query %s/%s: %w", projectID, agentID, err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			return nil, fmt.Errorf("quest: orders: scan: %w", err)
		}
		ids = append(ids, tid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("quest: orders: iterate: %w", err)
	}

	out := make([]*Quest, 0, len(ids))
	for _, tid := range ids {
		q, err := Load(ctx, db, projectID, tid)
		if err != nil {
			return nil, fmt.Errorf("quest: orders: load %s: %w", tid, err)
		}
		out = append(out, q)
	}
	return out, nil
}

// resolveAgent resolves the agent identity via the documented fallback chain.
func resolveAgent(agent string) string {
	if a := strings.TrimSpace(agent); a != "" {
		return a
	}
	if a := os.Getenv("PM_OWNER"); a != "" {
		return a
	}
	if a := os.Getenv("GUILD_AGENT"); a != "" {
		return a
	}
	if a := os.Getenv("USER"); a != "" {
		return a
	}
	return "agent"
}
