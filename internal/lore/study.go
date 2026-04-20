package lore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrEntryNotFound is returned by Study / Oath / helpers when an entry id
// does not resolve to a row.
var ErrEntryNotFound = errors.New("lore: entry not found")

// StudyResult is the full shape `lore study LORE-N` returns: the entry
// itself, every direct informs/supersedes/contradicts edge in both
// directions (NOT transitively — link propagation is disabled by design),
// and the top-ranked linked entry so the CLI can print a linked-entry
// footer without a second query.
type StudyResult struct {
	Entry *Entry

	// Linked is every direct edge touching Entry.ID, both incoming and
	// outgoing, resolved to the counterparty entry for display.
	Linked []LinkedEntry

	// TopLinked is the highest-priority linked entry for the CLI footer.
	// Priority order: outgoing edges rank above incoming; `supersedes`
	// outranks `informs`. nil when Linked is empty.
	TopLinked *LinkedEntry
}

// LinkedEntry is one edge from the entry being studied, resolved to the
// counterparty row and the edge metadata.
type LinkedEntry struct {
	Entry     *Entry
	Relation  Relation
	Direction EdgeDirection
}

// EdgeDirection labels whether the edge points AWAY from the studied
// entry (outgoing) or TOWARD it (incoming).
type EdgeDirection string

const (
	EdgeOutgoing EdgeDirection = "outgoing"
	EdgeIncoming EdgeDirection = "incoming"
)

// studyEntryQuery reads one entry by id. Not project-scoped because
// Study supports cross-project studies (a user can hold an LORE-N from
// any project); the caller can layer project enforcement on top.
const studyEntryQuery = `SELECT ` + entryColumns + ` FROM entries e WHERE e.id = ?`

// studyLinksQuery returns every direct edge in either direction. No
// recursion — link propagation is disabled by design.
const studyLinksQuery = `
	SELECT el.relation, el.to_id AS other_id, 'outgoing' AS direction
	FROM entry_links el WHERE el.from_id = ?
	UNION ALL
	SELECT el.relation, el.from_id AS other_id, 'incoming' AS direction
	FROM entry_links el WHERE el.to_id = ?`

// Study returns the full detail of one entry plus its direct link
// graph. It also bumps the access counter on the studied entry, not
// the linked entries.
func Study(ctx context.Context, db *sql.DB, id int64) (*StudyResult, error) {
	if db == nil {
		return nil, fmt.Errorf("lore: study: nil db")
	}
	if id <= 0 {
		return nil, fmt.Errorf("lore: study: invalid id %d", id)
	}

	e := &Entry{}
	row := db.QueryRowContext(ctx, studyEntryQuery, id)
	if err := scanEntry(row, e); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrEntryNotFound, formatEntryID(id))
		}
		// scanEntry wraps the underlying error; detect sql.ErrNoRows
		// through the wrapper too.
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrEntryNotFound, formatEntryID(id))
		}
		return nil, err
	}

	linked, err := loadLinkedEntries(ctx, db, id)
	if err != nil {
		return nil, err
	}

	result := &StudyResult{Entry: e, Linked: linked}
	if top := pickTopLinked(linked); top != nil {
		result.TopLinked = top
	}

	// Increment access counter on the studied entry only.
	if err := bumpSingleAccess(ctx, db, id, time.Now().UTC()); err != nil {
		return result, nil // best-effort — don't fail the read on counter bump
	}
	return result, nil
}

func loadLinkedEntries(ctx context.Context, db *sql.DB, id int64) ([]LinkedEntry, error) {
	rows, err := db.QueryContext(ctx, studyLinksQuery, id, id)
	if err != nil {
		return nil, fmt.Errorf("lore: study: links query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type edge struct {
		relation  Relation
		otherID   int64
		direction EdgeDirection
	}
	var edges []edge
	for rows.Next() {
		var relation string
		var otherID int64
		var direction string
		if err := rows.Scan(&relation, &otherID, &direction); err != nil {
			return nil, fmt.Errorf("lore: study: scan link: %w", err)
		}
		edges = append(edges, edge{Relation(relation), otherID, EdgeDirection(direction)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lore: study: iterate links: %w", err)
	}
	if len(edges) == 0 {
		return nil, nil
	}

	// Resolve counterparty entries in one batch.
	linked := make([]LinkedEntry, 0, len(edges))
	for _, e := range edges {
		other := &Entry{}
		otherRow := db.QueryRowContext(ctx, studyEntryQuery, e.otherID)
		if err := scanEntry(otherRow, other); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// orphan edge — skip but keep going.
				continue
			}
			return nil, err
		}
		linked = append(linked, LinkedEntry{
			Entry:     other,
			Relation:  e.relation,
			Direction: e.direction,
		})
	}
	return linked, nil
}

// pickTopLinked picks the single "highest-priority" linked entry for
// the clear-top-1 footer. The ordering is deliberately simple so the
// CLI footer is deterministic:
//  1. outgoing edges rank above incoming
//  2. supersedes > informs > contradicts
//  3. within the same tier, the highest-access entry wins (tracks
//     "most-used")
//  4. final tiebreaker: highest entry.id (most-recent)
//
// Returns nil when linked is empty.
func pickTopLinked(linked []LinkedEntry) *LinkedEntry {
	if len(linked) == 0 {
		return nil
	}
	best := 0
	for i := 1; i < len(linked); i++ {
		if linkedBetter(linked[i], linked[best]) {
			best = i
		}
	}
	out := linked[best]
	return &out
}

func linkedBetter(a, b LinkedEntry) bool {
	if a.Direction != b.Direction {
		// outgoing outranks incoming
		return a.Direction == EdgeOutgoing
	}
	if r := relationPriority(a.Relation) - relationPriority(b.Relation); r != 0 {
		return r > 0
	}
	if a.Entry.AccessCount != b.Entry.AccessCount {
		return a.Entry.AccessCount > b.Entry.AccessCount
	}
	return a.Entry.ID > b.Entry.ID
}

func relationPriority(r Relation) int {
	switch r {
	case RelationSupersedes:
		return 2
	case RelationInforms:
		return 1
	case RelationContradicts:
		return 0
	}
	return -1
}

// bumpSingleAccess increments access_count on one entry.
func bumpSingleAccess(ctx context.Context, db *sql.DB, id int64, now time.Time) error {
	_, err := db.ExecContext(ctx,
		`UPDATE entries SET access_count = access_count + 1, last_accessed_at = ? WHERE id = ?`,
		now.Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("lore: study: bump access: %w", err)
	}
	return nil
}
