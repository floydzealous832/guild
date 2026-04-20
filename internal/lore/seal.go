package lore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Seal marks entry id as archived in ProjectID's scope. Returns the
// post-seal Entry so callers can print the updated status.
//
// Sealing is idempotent: sealing an already-archived entry succeeds but
// bumps updated_at. We intentionally don't guard against that to simplify
// the CLI surface (no "entry already sealed" error-case branch).
func Seal(ctx context.Context, db *sql.DB, id int64, projectID string, now time.Time) (*Entry, error) {
	if db == nil {
		return nil, fmt.Errorf("lore: seal: nil db")
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("%w: project id", ErrMissingField)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	res, err := db.ExecContext(ctx,
		`UPDATE entries
		   SET status = 'archived', updated_at = ?
		 WHERE id = ? AND project_id = ?`,
		now.Format(time.RFC3339), id, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("lore: seal: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("lore: seal: rows affected: %w", err)
	}
	if n == 0 {
		return nil, fmt.Errorf("%w: %s (project %s)", ErrEntryNotFound, EntryID(id), projectID)
	}
	return loadEntry(ctx, db, id)
}
