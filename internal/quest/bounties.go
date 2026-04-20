package quest

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ParallelPair names two quests that have zero file overlap and no
// dependency conflict — they can run concurrently.
type ParallelPair struct {
	A string // always the TopQuest ID
	B string // the parallel candidate ID
}

// BountiesResult is the fully-loaded session-start context. Every
// field is optional: absent data is left nil/empty so callers
// render only what's available.
type BountiesResult struct {
	// LastBriefAgent, LastBriefText, LastBriefAt: most recent brief.
	// All empty if no brief has been written yet.
	LastBriefAgent string
	LastBriefText  string
	LastBriefAt    string // ISO-8601 truncated to minute: "2006-01-02T15:04"

	// Oath is the list of current `principle` lore entries.
	// Populated when an OathLoader is provided.
	Oath []OathEntry

	// Echoes is the list of stale lore entries.
	// Populated when an EchoLoader is provided.
	Echoes []EchoEntry

	// TopQuest is the highest-priority unclaimed `next` quest.
	// Nil when there are no open unclaimed quests.
	TopQuest *Quest

	// AllNext lists ALL unclaimed `next` quests sorted by priority + id.
	// Used by the CLI to display parallelism hints without a second query.
	AllNext []*Quest

	// ParallelCount is the total number of quests in AllNext (excluding
	// TopQuest) that can run in parallel with TopQuest: no shared files
	// AND no dependency conflict (neither depends on the other).
	ParallelCount int

	// ParallelPairs names the first min(ParallelCount, 5) parallel
	// candidates for display.
	ParallelPairs []ParallelPair

	// NoUnclaimed is set when there are zero unclaimed next quests.
	NoUnclaimed bool
}

// OathEntry is a thin projection of a lore principle for bounties output.
// We keep it self-contained in the quest package to avoid a lore import.
type OathEntry struct {
	Title   string
	Summary string
}

// EchoEntry is a thin projection of a stale lore entry for bounties output.
type EchoEntry struct {
	Title  string
	Reason string
}

// OathLoader is a function that returns the active oath entries for a
// project. Bounties accepts this as a parameter so the quest package
// doesn't import the lore package — the CLI layer wires them together.
type OathLoader func(ctx context.Context, project string) ([]OathEntry, error)

// EchoLoader is a function that returns fading echo entries for a
// project.
type EchoLoader func(ctx context.Context, project string) ([]EchoEntry, error)

// Bounties loads the full session-start context for projectID.
//
// When briefOnly=true, only the LastBrief* fields are populated — the
// "catch me up" mode for agents that already know their oath and tasks.
//
// oathLoader and echoLoader may be nil; when nil, Oath and Echoes are
// left empty (graceful degradation when lore DB is unavailable).
func Bounties(
	ctx context.Context,
	db *sql.DB,
	projectID string,
	briefOnly bool,
	oathLoader OathLoader,
	echoLoader EchoLoader,
) (*BountiesResult, error) {
	if db == nil {
		return nil, fmt.Errorf("quest: bounties: nil db")
	}
	if projectID = strings.TrimSpace(projectID); projectID == "" {
		return nil, fmt.Errorf("quest: bounties: empty project_id")
	}

	result := &BountiesResult{}

	// Always load the last brief — it's shown in both full and brief-only mode.
	agent, text, at, err := LastBrief(ctx, db, projectID)
	if err != nil {
		return nil, fmt.Errorf("quest: bounties: last brief: %w", err)
	}
	if text != "" {
		result.LastBriefAgent = agent
		result.LastBriefText = text
		if !at.IsZero() {
			// Truncate to minute precision.
			result.LastBriefAt = at.UTC().Format("2006-01-02T15:04")
		}
	}

	if briefOnly {
		return result, nil
	}

	// Load oath (principles). Degrade gracefully on error.
	if oathLoader != nil {
		oaths, oErr := oathLoader(ctx, projectID)
		if oErr == nil {
			result.Oath = oaths
		}
	}

	// Load echoes (fading lore). Degrade gracefully on error.
	if echoLoader != nil {
		echoes, eErr := echoLoader(ctx, projectID)
		if eErr == nil {
			result.Echoes = echoes
		}
	}

	// Load all unclaimed next quests, sorted by priority.
	allNext, err := loadUnclaimedNext(ctx, db, projectID)
	if err != nil {
		return nil, fmt.Errorf("quest: bounties: load next quests: %w", err)
	}

	if len(allNext) == 0 {
		result.NoUnclaimed = true
		return result, nil
	}

	result.AllNext = allNext
	result.TopQuest = allNext[0]

	// Parallelism detection: for each candidate in allNext[1:], check
	// whether it can run alongside TopQuest:
	//   - no shared files (file set intersection is empty)
	//   - no dependency conflict: TopQuest.ID not in candidate.DependsOn
	//     AND candidate.ID not in TopQuest.DependsOn
	topID := allNext[0].ID
	topFiles := stringSet(allNext[0].Files)
	topDeps := stringSet(allNext[0].DependsOn)

	var parallelCandidates []*Quest
	for _, q := range allNext[1:] {
		cFiles := stringSet(q.Files)
		cDeps := stringSet(q.DependsOn)
		noFileOverlap := !setsIntersect(topFiles, cFiles)
		noDepConflict := !topDeps[q.ID] && !cDeps[topID]
		if noFileOverlap && noDepConflict {
			parallelCandidates = append(parallelCandidates, q)
		}
	}

	result.ParallelCount = len(parallelCandidates)

	// Collect up to 5 pairs for display.
	maxShow := len(parallelCandidates)
	if maxShow > 5 {
		maxShow = 5
	}
	pairs := make([]ParallelPair, maxShow)
	for i, q := range parallelCandidates[:maxShow] {
		pairs[i] = ParallelPair{A: topID, B: q.ID}
	}
	result.ParallelPairs = pairs

	// Record a pm_next_called event so future bounties calls can surface
	// recently-unblocked tasks. Non-fatal if it fails.
	_ = recordBountiesCall(ctx, db, projectID, topID)

	return result, nil
}

// loadUnclaimedNext returns every quest in the project with status=next
// and claimed_by IS NULL, fully-resolved via Load, sorted by priority
// (P0 > P1 > P2 > ...) then by task_id.
func loadUnclaimedNext(ctx context.Context, db *sql.DB, projectID string) ([]*Quest, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT task_id FROM task_status
		 WHERE project_id = ? AND status = 'next' AND claimed_by IS NULL
		 ORDER BY task_id ASC`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("quest: load unclaimed next: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			return nil, fmt.Errorf("quest: load unclaimed next: scan: %w", err)
		}
		ids = append(ids, tid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("quest: load unclaimed next: iterate: %w", err)
	}

	quests := make([]*Quest, 0, len(ids))
	for _, tid := range ids {
		q, err := Load(ctx, db, projectID, tid)
		if err != nil {
			return nil, fmt.Errorf("quest: load unclaimed next: load %s: %w", tid, err)
		}
		quests = append(quests, q)
	}

	// Sort by priority rank (PriorityOrder) then by ID for determinism.
	sort.SliceStable(quests, func(i, j int) bool {
		ri := PriorityOrder(quests[i].Priority)
		rj := PriorityOrder(quests[j].Priority)
		if ri != rj {
			return ri < rj
		}
		return quests[i].ID < quests[j].ID
	})

	return quests, nil
}

// recordBountiesCall writes a pm_next_called event for the top quest.
func recordBountiesCall(ctx context.Context, db *sql.DB, projectID, topTaskID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := emitEvent(ctx, tx, projectID, topTaskID, EventPMNextCalled, "quest", "", now); err != nil {
		return err
	}
	return tx.Commit()
}

// stringSet converts a slice of strings into a membership set.
func stringSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		if s != "" {
			m[s] = true
		}
	}
	return m
}

// setsIntersect returns true when the two sets share at least one key.
func setsIntersect(a, b map[string]bool) bool {
	// Iterate the smaller set for performance.
	if len(a) > len(b) {
		a, b = b, a
	}
	for k := range a {
		if b[k] {
			return true
		}
	}
	return false
}
