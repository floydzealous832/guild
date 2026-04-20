package quest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// snapshotSchema is the current schema_version for snapshot.json.
const snapshotSchema = 1

// snapshot is the top-level shape of .guild/snapshot.json.
// The quest section is embedded as a raw message so we can preserve
// the lore section verbatim when reading and writing.
type snapshot struct {
	SchemaVersion int             `json:"schema_version"`
	SnapshotAt    string          `json:"snapshot_at"`
	ProducedBy    string          `json:"produced_by"`
	Lore          json.RawMessage `json:"lore"`
	Quest         *questSection   `json:"quest"`
}

// questSection is the quest-half of the snapshot.
type questSection struct {
	ProjectID  string          `json:"project_id"`
	TaskStatus []taskStatusRow `json:"task_status"`
	TaskNotes  []taskNoteRow   `json:"task_notes"`
	TaskEvents []taskEventRow  `json:"task_events"`
}

type taskStatusRow struct {
	ProjectID string `json:"project_id"`
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	ClaimedBy string `json:"claimed_by,omitempty"`
	ClaimedAt string `json:"claimed_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type taskNoteRow struct {
	ID        int64  `json:"id"`
	ProjectID string `json:"project_id"`
	TaskID    string `json:"task_id"`
	AgentID   string `json:"agent_id"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at,omitempty"`
}

type taskEventRow struct {
	ID        int64  `json:"id"`
	ProjectID string `json:"project_id"`
	TaskID    string `json:"task_id"`
	Event     string `json:"event"`
	AgentID   string `json:"agent_id,omitempty"`
	Data      string `json:"data,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// Archive writes the quest half of <snapshotPath> while PRESERVING any
// existing lore:[...] section. Atomic-rename via .tmp.
//
// snapshotPath defaults to "<project.path>/.guild/snapshot.json" when
// passed as "". The directory is created if it doesn't exist.
//
// If the file doesn't exist, it is created with an empty lore:[] section.
func Archive(ctx context.Context, db *sql.DB, projectID, snapshotPath string) error {
	if db == nil {
		return fmt.Errorf("quest: archive: nil db")
	}
	if projectID = strings.TrimSpace(projectID); projectID == "" {
		return fmt.Errorf("quest: archive: empty project_id")
	}

	// Resolve snapshot path from project record if not provided.
	if snapshotPath == "" {
		row := db.QueryRowContext(ctx,
			`SELECT path FROM projects WHERE id = ?`, projectID,
		)
		var projPath string
		if err := row.Scan(&projPath); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("quest: archive: project %q not registered", projectID)
			}
			return fmt.Errorf("quest: archive: lookup project path: %w", err)
		}
		snapshotPath = filepath.Join(projPath, ".guild", "snapshot.json")
	}

	// Ensure directory exists.
	dir := filepath.Dir(snapshotPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("quest: archive: create dir %s: %w", dir, err)
	}

	// --- Read existing lore section to preserve it ---
	var existingLore json.RawMessage = json.RawMessage("[]")
	if existing, err := os.ReadFile(snapshotPath); err == nil {
		var prev snapshot
		if jsonErr := json.Unmarshal(existing, &prev); jsonErr == nil && prev.Lore != nil {
			// Preserve lore only if it's a valid non-null value.
			if string(prev.Lore) != "null" && len(prev.Lore) > 0 {
				existingLore = prev.Lore
			}
		}
	}

	// --- Load quest data ---
	statuses, err := loadTaskStatuses(ctx, db, projectID)
	if err != nil {
		return err
	}
	notes, err := loadTaskNotes(ctx, db, projectID)
	if err != nil {
		return err
	}
	events, err := loadTaskEvents(ctx, db, projectID)
	if err != nil {
		return err
	}

	snap := &snapshot{
		SchemaVersion: snapshotSchema,
		SnapshotAt:    time.Now().UTC().Format(time.RFC3339),
		ProducedBy:    "guild 0.1.0",
		Lore:          existingLore,
		Quest: &questSection{
			ProjectID:  projectID,
			TaskStatus: statuses,
			TaskNotes:  notes,
			TaskEvents: events,
		},
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("quest: archive: marshal: %w", err)
	}
	data = append(data, '\n')

	// Atomic write via .tmp + rename.
	tmp := snapshotPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("quest: archive: write tmp: %w", err)
	}
	if err := os.Rename(tmp, snapshotPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("quest: archive: rename to %s: %w", snapshotPath, err)
	}
	return nil
}

func loadTaskStatuses(ctx context.Context, db *sql.DB, projectID string) ([]taskStatusRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT project_id, task_id, status,
		        COALESCE(claimed_by,''), COALESCE(claimed_at,''), COALESCE(updated_at,'')
		 FROM task_status
		 WHERE project_id = ?
		 ORDER BY task_id ASC`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("quest: archive: query task_status: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []taskStatusRow
	for rows.Next() {
		var r taskStatusRow
		if err := rows.Scan(&r.ProjectID, &r.TaskID, &r.Status,
			&r.ClaimedBy, &r.ClaimedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("quest: archive: scan task_status: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadTaskNotes(ctx context.Context, db *sql.DB, projectID string) ([]taskNoteRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, task_id, agent_id, note, COALESCE(created_at,'')
		 FROM task_notes
		 WHERE project_id = ?
		 ORDER BY id ASC`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("quest: archive: query task_notes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []taskNoteRow
	for rows.Next() {
		var r taskNoteRow
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.TaskID,
			&r.AgentID, &r.Note, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("quest: archive: scan task_notes: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadTaskEvents(ctx context.Context, db *sql.DB, projectID string) ([]taskEventRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, task_id, event,
		        COALESCE(agent_id,''), COALESCE(data,''), COALESCE(created_at,'')
		 FROM task_events
		 WHERE project_id = ?
		 ORDER BY id ASC`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("quest: archive: query task_events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []taskEventRow
	for rows.Next() {
		var r taskEventRow
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.TaskID, &r.Event,
			&r.AgentID, &r.Data, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("quest: archive: scan task_events: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
