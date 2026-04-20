package lore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// InquestBucket classifies a principle's word count into one of three bands.
//
//	≤30  → short  (crisp; ideal)
//	31-60 → medium (acceptable)
//	>60  → bloat  (narrative-shaped; flag for reclassification)
type InquestBucket string

const (
	BucketShort  InquestBucket = "short"  // ≤30 words
	BucketMedium InquestBucket = "medium" // 31-60 words
	BucketBloat  InquestBucket = "bloat"  // >60 words
)

// InquestShortBoundary is the upper inclusive word-count for "short" principles.
// Word counts ≤ this value are in the short bucket.
const InquestShortBoundary = 30

// InquestBloatBoundary is the minimum word count (exclusive) that puts a
// principle into the bloat bucket. Callers should prefer
// cfg.Inscribe.PrincipleMaxWords to respect per-project overrides; this
// constant is the built-in default.
const InquestBloatBoundary = 60

// InquestRow is one principle entry with its word count and bucket.
type InquestRow struct {
	EntryID   int64
	ProjectID string
	Title     string
	WordCount int
	Bucket    InquestBucket
}

// InquestProjectStats summarises one project's principle word-count distribution.
type InquestProjectStats struct {
	ProjectID  string
	TotalOaths int
	TotalWords int
	Short      int // ≤30 words
	Medium     int // 31-60 words
	Bloat      int // >60 words
}

// InquestResult is the full output of Inquest.
type InquestResult struct {
	// Projects contains per-project statistics, ordered by project id.
	Projects []InquestProjectStats

	// BloatEntries is the subset of principle entries with word count > bloat boundary,
	// sorted by word count descending (highest bloat first).
	BloatEntries []InquestRow

	// TotalOaths is the sum of all principles across all scanned projects.
	TotalOaths int
	// TotalWords is the combined word count across all principles.
	TotalWords int

	// bloatBoundary records which boundary was used so callers can print
	// the right threshold in their output.
	BloatBoundary int
}

// Inquest audits the oath wall for narrative-bloat principles.
//
// When allProjects is true, every registered project's principles are scanned.
// When false, only the project owning the database connection is scanned — the
// caller is responsible for resolving projectID to the correct value before
// calling.
//
// bloatBoundary is the word count above which a principle is considered bloat.
// Pass cfg.Inscribe.PrincipleMaxWords here (default: InquestBloatBoundary = 60).
//
// The returned InquestResult contains per-project stats and the list of bloat
// candidates sorted by word count descending.
func Inquest(ctx context.Context, db *sql.DB, projectID string, allProjects bool, bloatBoundary int) (*InquestResult, error) {
	if db == nil {
		return nil, fmt.Errorf("lore: inquest: nil db")
	}
	if bloatBoundary <= 0 {
		bloatBoundary = InquestBloatBoundary
	}

	var rows *sql.Rows
	var err error

	if allProjects {
		rows, err = db.QueryContext(ctx,
			`SELECT e.id, p.id AS project_id, e.title, e.summary
			   FROM entries e
			   JOIN projects p ON e.project_id = p.id
			  WHERE e.kind = 'principle'
			    AND e.status = 'current'
			  ORDER BY p.id, e.id`,
		)
	} else {
		if strings.TrimSpace(projectID) == "" {
			return nil, fmt.Errorf("lore: inquest: projectID required when allProjects=false")
		}
		rows, err = db.QueryContext(ctx,
			`SELECT e.id, ? AS project_id, e.title, e.summary
			   FROM entries e
			  WHERE e.project_id = ?
			    AND e.kind = 'principle'
			    AND e.status = 'current'
			  ORDER BY e.id`,
			projectID, projectID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("lore: inquest: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Collect all rows first, then aggregate.
	type rawRow struct {
		id        int64
		projectID string
		title     string
		summary   string
	}
	var rawRows []rawRow
	for rows.Next() {
		var r rawRow
		if err := rows.Scan(&r.id, &r.projectID, &r.title, &r.summary); err != nil {
			return nil, fmt.Errorf("lore: inquest: scan: %w", err)
		}
		rawRows = append(rawRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lore: inquest: iterate: %w", err)
	}

	// Aggregate per project.
	type accumulator struct {
		stats   InquestProjectStats
		entries []InquestRow
	}
	byProject := make(map[string]*accumulator)
	var projectOrder []string

	for _, r := range rawRows {
		words := countWords(r.title) + countWords(r.summary)
		bucket := classifyWordCount(words, bloatBoundary)
		row := InquestRow{
			EntryID:   r.id,
			ProjectID: r.projectID,
			Title:     r.title,
			WordCount: words,
			Bucket:    bucket,
		}

		acc, exists := byProject[r.projectID]
		if !exists {
			acc = &accumulator{stats: InquestProjectStats{ProjectID: r.projectID}}
			byProject[r.projectID] = acc
			projectOrder = append(projectOrder, r.projectID)
		}
		acc.stats.TotalOaths++
		acc.stats.TotalWords += words
		switch bucket {
		case BucketShort:
			acc.stats.Short++
		case BucketMedium:
			acc.stats.Medium++
		case BucketBloat:
			acc.stats.Bloat++
		}
		acc.entries = append(acc.entries, row)
	}

	// Sort project order lexicographically.
	sortProjectIDs(projectOrder)

	result := &InquestResult{
		BloatBoundary: bloatBoundary,
	}
	for _, pid := range projectOrder {
		acc := byProject[pid]
		result.Projects = append(result.Projects, acc.stats)
		result.TotalOaths += acc.stats.TotalOaths
		result.TotalWords += acc.stats.TotalWords
		for _, row := range acc.entries {
			if row.Bucket == BucketBloat {
				result.BloatEntries = append(result.BloatEntries, row)
			}
		}
	}

	// Sort bloat list by word count descending (highest bloat first).
	sortBloatDesc(result.BloatEntries)

	return result, nil
}

// classifyWordCount assigns a bucket based on the word count and bloat boundary.
// Short: ≤ InquestShortBoundary; Bloat: > bloatBoundary; Medium: in between.
func classifyWordCount(words, bloatBoundary int) InquestBucket {
	if words <= InquestShortBoundary {
		return BucketShort
	}
	if words > bloatBoundary {
		return BucketBloat
	}
	return BucketMedium
}

// sortBloatDesc sorts an InquestRow slice by WordCount descending in-place.
// Uses a simple insertion sort; slice is always small.
func sortBloatDesc(rows []InquestRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j-1].WordCount < rows[j].WordCount; j-- {
			rows[j-1], rows[j] = rows[j], rows[j-1]
		}
	}
}

// sortProjectIDs sorts a string slice in-place using insertion sort.
// Kept inside the lore package to avoid importing sort for a small, bounded slice.
func sortProjectIDs(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
