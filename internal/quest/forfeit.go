package quest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Forfeit releases a claim on taskID without marking it done. Any notes
// already attached persist for the next agent who accepts. Note can be
// empty; when non-empty it is stored as "[released] <note>" so the
// reason survives into subsequent scrolls.
//
// The status is forced back to 'next' even if the quest was never
// accepted in the first place — this is a regression-to-clean-slate
// primitive, not a state-machine guard. claimed_by / claimed_at clear
// unconditionally.
func Forfeit(ctx context.Context, db *sql.DB, projectID, taskID, note string) (*Quest, error) {
	if db == nil {
		return nil, fmt.Errorf("quest: forfeit: nil db")
	}
	if projectID = strings.TrimSpace(projectID); projectID == "" {
		return nil, fmt.Errorf("quest: forfeit: empty project_id")
	}
	taskID = strings.ToUpper(strings.TrimSpace(taskID))
	if taskID == "" {
		return nil, fmt.Errorf("quest: forfeit: empty task_id")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("quest: forfeit: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var existStatus, existOwner sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT status, claimed_by FROM task_status
		 WHERE project_id = ? AND task_id = ?`,
		projectID, taskID,
	).Scan(&existStatus, &existOwner)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, taskID)
		}
		return nil, fmt.Errorf("quest: forfeit: probe existing: %w", err)
	}
	owner := existOwner.String
	if owner == "" {
		owner = "agent"
	}

	// Optional note attached first so the `[released]` note precedes
	// any downstream pulse queries that scan forward-in-time.
	if n := strings.TrimSpace(note); n != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO task_notes (project_id, task_id, agent_id, note, created_at)
			 VALUES (?, ?, ?, ?, ?)`,
			projectID, taskID, owner, "[released] "+n, now,
		); err != nil {
			return nil, fmt.Errorf("quest: forfeit: insert release note: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE task_status
		 SET status = 'next',
		     claimed_by = NULL,
		     claimed_at = NULL,
		     updated_at = ?
		 WHERE project_id = ? AND task_id = ?`,
		now, projectID, taskID,
	); err != nil {
		return nil, fmt.Errorf("quest: forfeit: update: %w", err)
	}

	if err := emitEvent(ctx, tx, projectID, taskID, "released", owner, note, now); err != nil {
		return nil, err
	}

	result, err := loadTx(ctx, tx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("quest: forfeit: commit: %w", err)
	}
	return result, nil
}
