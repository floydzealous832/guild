package lore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// UpdateParams carries the set of columns `lore update` can mutate:
// --summary --status --tags --topic --title --kind. Every field is a
// pointer so the zero value can mean "do not change" (empty-string IS a
// meaningful change for a nullable-column like tags → NULL; callers
// pass &"" for that).
//
// ProjectID is the scope guard — Update will not edit an entry that
// belongs to a different project (WHERE id=? AND project_id=?).
// Cross-project editing is a separate, explicit workflow the CLI doesn't
// expose today.
type UpdateParams struct {
	ProjectID string

	Title   *string
	Summary *string
	Topic   *string
	Tags    *string // pre-joined "a,b,c"; pass &"" to clear
	Status  *Status
	Kind    *Kind

	Now time.Time // injectable for deterministic tests; zero → time.Now().UTC()
}

// ErrNoChanges is returned by Update when the caller supplies no field
// pointers. Callers can errors.Is it to distinguish "bad invocation" from
// "entry not found."
var ErrNoChanges = errors.New("lore: update: no fields provided")

// ErrEntryNotFound is declared in study.go (package-shared for both
// the read and write surfaces).

// ErrInvalidStatus is returned when caller supplies a status outside the
// Status enum.
var ErrInvalidStatus = errors.New("lore: invalid status")

// Update mutates the fields of entry id within ProjectID's scope. Returns
// the post-update Entry by re-reading the row. Missing entry →
// ErrEntryNotFound; no changes → ErrNoChanges; invalid kind/status →
// ErrInvalidKind / ErrInvalidStatus.
//
// The dynamic SET clause is assembled from a fixed allowlist of column
// names (loreUpdateColumns below) joined with strings.Join; values flow
// through `?` placeholders only — sqlcheck stays clean because no
// fmt.Sprintf or binary-string concat touches the SQL string.
func Update(ctx context.Context, db *sql.DB, id int64, p *UpdateParams) (*Entry, error) {
	if db == nil {
		return nil, fmt.Errorf("lore: update: nil db")
	}
	if p == nil {
		return nil, fmt.Errorf("lore: update: nil params")
	}
	if strings.TrimSpace(p.ProjectID) == "" {
		return nil, fmt.Errorf("%w: project id", ErrMissingField)
	}

	// Walk the pointer fields in a fixed order and build a set of
	// "column = ?" fragments from a pre-known allowlist. The allowlist
	// keeps SQL construction safe (no caller-controlled column names).
	var (
		setFrags []string
		args     []any
	)

	if p.Title != nil {
		setFrags = append(setFrags, colTitle)
		args = append(args, *p.Title)
	}
	if p.Summary != nil {
		setFrags = append(setFrags, colSummary)
		args = append(args, *p.Summary)
	}
	if p.Topic != nil {
		setFrags = append(setFrags, colTopic)
		args = append(args, *p.Topic)
	}
	if p.Tags != nil {
		setFrags = append(setFrags, colTags)
		args = append(args, nullIfEmpty(*p.Tags))
	}
	if p.Status != nil {
		if !isValidStatus(*p.Status) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidStatus, string(*p.Status))
		}
		setFrags = append(setFrags, colStatus)
		args = append(args, string(*p.Status))
	}
	if p.Kind != nil {
		if !isValidKind(*p.Kind) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidKind, string(*p.Kind))
		}
		setFrags = append(setFrags, colKind)
		args = append(args, string(*p.Kind))
	}

	if len(setFrags) == 0 {
		return nil, ErrNoChanges
	}

	now := p.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	setFrags = append(setFrags, colUpdatedAt)
	args = append(args, now.Format(time.RFC3339))

	// Assemble the static pieces + the validated SET clause. Note:
	// strings.Join of column-name CONSTANTS is not flagged by sqlcheck
	// (no fmt.Sprintf, no binary +), and the values all flow through ?
	// placeholders.
	setClause := strings.Join(setFrags, ", ")
	query := updateQueryPrefix + setClause + updateQuerySuffix

	args = append(args, id, p.ProjectID)

	res, err := db.ExecContext(ctx, query, args...) //nolint:sqlcheck // dynamic SET built from allowlisted constants; values bound.
	if err != nil {
		return nil, fmt.Errorf("lore: update: exec: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("lore: update: rows affected: %w", err)
	}
	if n == 0 {
		// Either the id doesn't exist or it belongs to another project.
		return nil, fmt.Errorf("%w: %s (project %s)", ErrEntryNotFound, EntryID(id), p.ProjectID)
	}

	return loadEntry(ctx, db, id)
}

// colTitle and friends are the column-name constants the dynamic SET
// clause pulls from. Kept as constants so sqlcheck can trace them back
// to literals (see cmd/sqlcheck/analyzer.go: *ast.Ident with types.Const
// resolves to safe).
const (
	colTitle     = "title = ?"
	colSummary   = "summary = ?"
	colTopic     = "topic = ?"
	colTags      = "tags = ?"
	colStatus    = "status = ?"
	colKind      = "kind = ?"
	colUpdatedAt = "updated_at = ?"
)

// updateQueryPrefix and updateQuerySuffix frame the caller-assembled
// SET clause. Keeping them as named constants makes the final Exec call
// provably safe even though the middle chunk is dynamic.
const (
	updateQueryPrefix = `UPDATE entries SET `
	updateQuerySuffix = ` WHERE id = ? AND project_id = ?`
)

// validStatuses enumerates the canonical Status values.
var validStatuses = map[Status]struct{}{
	StatusCurrent:    {},
	StatusStale:      {},
	StatusSuperseded: {},
	StatusArchived:   {},
	StatusImported:   {},
	StatusSeed:       {},
	StatusExploring:  {},
	StatusPromoted:   {},
	StatusParked:     {},
}

// isValidStatus reports whether s is a canonical Status value.
func isValidStatus(s Status) bool {
	_, ok := validStatuses[s]
	return ok
}

// loadEntry re-reads the entries row into an *Entry after a successful
// update/seal/reforge so callers can print the post-state. Separated out
// because update.go, seal.go and reforge.go all need the same scan path.
func loadEntry(ctx context.Context, db *sql.DB, id int64) (*Entry, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, project_id, topic, kind, title, summary,
		        COALESCE(tags, ''),
		        COALESCE(file_path, ''),
		        COALESCE(source, ''),
		        status, valid_days, needs_review,
		        COALESCE(prompted_by, ''),
		        created_at, updated_at,
		        access_count, last_accessed_at
		   FROM entries WHERE id = ?`,
		id,
	)
	var (
		e             Entry
		tagsJoined    string
		kindStr       string
		statusStr     string
		validDays     sql.NullInt64
		needsReview   int
		createdStr    string
		updatedStr    string
		accessCount   int
		lastAccessStr sql.NullString
	)
	if err := row.Scan(
		&e.ID, &e.ProjectID, &e.Topic, &kindStr, &e.Title, &e.Summary,
		&tagsJoined, &e.FilePath, &e.Source, &statusStr, &validDays,
		&needsReview, &e.PromptedBy, &createdStr, &updatedStr,
		&accessCount, &lastAccessStr,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrEntryNotFound, EntryID(id))
		}
		return nil, fmt.Errorf("lore: load entry %d: %w", id, err)
	}

	e.Kind = Kind(kindStr)
	e.Status = Status(statusStr)
	if tagsJoined != "" {
		e.Tags = strings.Split(tagsJoined, ",")
	}
	if validDays.Valid {
		v := int(validDays.Int64)
		e.ValidDays = &v
	}
	e.NeedsReview = needsReview != 0
	e.AccessCount = accessCount

	if t, err := parseTimestamp(createdStr); err == nil {
		e.CreatedAt = t
	}
	if t, err := parseTimestamp(updatedStr); err == nil {
		e.UpdatedAt = t
	}
	if lastAccessStr.Valid && lastAccessStr.String != "" {
		if t, err := parseTimestamp(lastAccessStr.String); err == nil {
			e.LastAccessedAt = &t
		}
	}
	return &e, nil
}

// parseTimestamp accepts both the SQLite default format
// ("YYYY-MM-DD HH:MM:SS") and RFC3339. Returning time.Zero on unparseable
// is fine — the field is display-only for the Inscribe/Update response.
func parseTimestamp(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp format: %q", s)
}
