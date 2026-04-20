package quest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Summon transfers ownership of questID to targetAgent. The quest is set
// to status=in_progress and claimed_by=targetAgent. An "assigned" event
// is emitted for the timeline.
//
// The original owner (or calling agent) is the emitter; targetAgent is
// the assignee recorded in both task_status and the event payload.
//
// callerAgent identifies who is issuing the summon (used for the event);
// if empty it defaults via journalAgent.
// Returns ErrNotFound when questID has no row in task_status.
func Summon(ctx context.Context, db *sql.DB, projectID, questID, targetAgent, callerAgent string) error {
	if db == nil {
		return fmt.Errorf("quest: summon: nil db")
	}
	if projectID = strings.TrimSpace(projectID); projectID == "" {
		return fmt.Errorf("quest: summon: empty project_id")
	}
	questID = strings.ToUpper(strings.TrimSpace(questID))
	if questID == "" {
		return fmt.Errorf("quest: summon: empty quest_id")
	}
	targetAgent = strings.TrimSpace(targetAgent)
	if targetAgent == "" {
		return fmt.Errorf("quest: summon: empty target agent")
	}
	callerAgent = journalAgent(callerAgent)

	// Verify quest exists.
	var existStatus sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT status FROM task_status WHERE project_id = ? AND task_id = ?`,
		projectID, questID,
	).Scan(&existStatus)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrNotFound, questID)
		}
		return fmt.Errorf("quest: summon: probe %s: %w", questID, err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("quest: summon: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`UPDATE task_status
		 SET status = 'in_progress', claimed_by = ?, claimed_at = ?, updated_at = ?
		 WHERE project_id = ? AND task_id = ?`,
		targetAgent, now, now, projectID, questID,
	); err != nil {
		return fmt.Errorf("quest: summon: update %s: %w", questID, err)
	}

	// Emit an "assigned" event: data contains the assignee so Scroll
	// can show "assigned → <agent>".
	if err := emitEvent(ctx, tx, projectID, questID, "assigned", callerAgent, targetAgent, now); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("quest: summon: commit: %w", err)
	}
	return nil
}
