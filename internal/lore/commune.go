package lore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// CommuneReport is the composite health-check output returned by Commune.
// It carries counts from all three sub-checks plus the list of auto-fixes
// applied when fix=true.
type CommuneReport struct {
	// Inquest results.
	TotalPrinciples int
	BloatCount      int          // principles > bloatBoundary (warn)
	SevereCount     int          // principles > severeBoundary (auto-fix candidate)
	BloatEntries    []InquestRow // all bloat entries (> bloatBoundary)

	// Meld results.
	DupPairCount int
	DriftCount   int // pairs with kind drift
	DupPairs     []MeldPair

	// Recall sanity.
	RecallSampleSize int
	RecallMisses     int
	RecallSkipped    bool // no entries with access_count > 0 yet

	// Auto-fix results (non-nil only when fix=true).
	FixesApplied []CommuneFix
}

// CommuneFix describes one auto-applied remediation.
type CommuneFix struct {
	Kind    string // "reclassify" | "reforge"
	EntryID int64
	Detail  string
}

// Commune is the composite lore health check: oath bloat + cross-project dups
// + recall sanity.
//
// bloatBoundary   = cfg.Inscribe.PrincipleMaxWords (default 60)
// severeBoundary  = cfg.Inscribe.BloatSevereThreshold (default 120)
//
// When fix=true, Commune auto-demotes principles with word count ≥ severeBoundary
// from kind=principle to kind=decision, and reforges exact-match dup pairs
// (older→superseded by newer).
func Commune(ctx context.Context, db *sql.DB, projectID string, allProjects, fix bool, bloatBoundary, severeBoundary int) (*CommuneReport, error) {
	if db == nil {
		return nil, fmt.Errorf("lore: commune: nil db")
	}
	if bloatBoundary <= 0 {
		bloatBoundary = InquestBloatBoundary
	}
	if severeBoundary <= 0 {
		severeBoundary = 120
	}

	report := &CommuneReport{}

	// --- Sub-check 1: oath bloat (Inquest) ---
	inqResult, err := Inquest(ctx, db, projectID, allProjects, bloatBoundary)
	if err != nil {
		return nil, fmt.Errorf("lore: commune: inquest: %w", err)
	}
	report.TotalPrinciples = inqResult.TotalOaths
	report.BloatCount = len(inqResult.BloatEntries)
	report.BloatEntries = inqResult.BloatEntries
	for _, e := range inqResult.BloatEntries {
		if e.WordCount > severeBoundary {
			report.SevereCount++
		}
	}

	// --- Sub-check 2: dedup (always cross-project) ---
	dupPairs, err := Meld(ctx, db, 1.0, true, "")
	if err != nil {
		return nil, fmt.Errorf("lore: commune: meld: %w", err)
	}
	report.DupPairs = dupPairs
	report.DupPairCount = len(dupPairs)
	for _, p := range dupPairs {
		if p.KindDrift {
			report.DriftCount++
		}
	}

	// --- Sub-check 3: recall sanity ---
	// Sample the top-10 most-accessed entries with non-trivial titles.
	sampleRows, err := db.QueryContext(ctx,
		`SELECT id, project_id, title
		   FROM entries
		  WHERE status = 'current'
		    AND length(title) > 12
		  ORDER BY access_count DESC
		  LIMIT 10`,
	)
	if err != nil {
		return nil, fmt.Errorf("lore: commune: recall sample: %w", err)
	}
	type sampleEntry struct {
		id        int64
		projectID string
		title     string
	}
	var samples []sampleEntry
	for sampleRows.Next() {
		var s sampleEntry
		if err := sampleRows.Scan(&s.id, &s.projectID, &s.title); err != nil {
			_ = sampleRows.Close()
			return nil, fmt.Errorf("lore: commune: recall scan: %w", err)
		}
		samples = append(samples, s)
	}
	_ = sampleRows.Close()
	if err := sampleRows.Err(); err != nil {
		return nil, fmt.Errorf("lore: commune: recall iterate: %w", err)
	}

	report.RecallSampleSize = len(samples)
	if len(samples) == 0 {
		report.RecallSkipped = true
	} else {
		for _, s := range samples {
			dedupQ := ftsDedupQuery(s.title)
			if dedupQ == "" {
				continue
			}
			var topID int64
			err := db.QueryRowContext(ctx,
				`SELECT e.id
				   FROM entries_fts
				   JOIN entries e ON e.id = entries_fts.rowid
				  WHERE entries_fts MATCH ?
				    AND e.project_id = ?
				    AND e.status IN ('current','seed','exploring','imported')
				  ORDER BY entries_fts.rank
				  LIMIT 1`,
				dedupQ, s.projectID,
			).Scan(&topID)
			if err != nil || topID != s.id {
				report.RecallMisses++
			}
		}
	}

	// --- Auto-fix path (fix=true) ---
	if fix {
		now := time.Now().UTC().Format(time.RFC3339)

		// Reclassify principles with word count ≥ severeBoundary → kind=decision.
		for _, e := range inqResult.BloatEntries {
			if e.WordCount <= severeBoundary {
				continue
			}
			_, err := db.ExecContext(ctx,
				`UPDATE entries SET kind = 'decision', updated_at = ? WHERE id = ?`,
				now, e.EntryID,
			)
			if err != nil {
				return nil, fmt.Errorf("lore: commune: fix reclassify %s: %w", formatEntryID(e.EntryID), err)
			}
			report.FixesApplied = append(report.FixesApplied, CommuneFix{
				Kind:    "reclassify",
				EntryID: e.EntryID,
				Detail:  fmt.Sprintf("principle → decision (%dw)", e.WordCount),
			})
		}

		// Reforge exact-match dup pairs: mark older (lower id) as superseded.
		for _, p := range dupPairs {
			// Older entry is lower id — supersede it with newer.
			_, err := db.ExecContext(ctx,
				`UPDATE entries SET status = 'superseded', updated_at = ? WHERE id = ?`,
				now, p.LeftID,
			)
			if err != nil {
				return nil, fmt.Errorf("lore: commune: fix reforge %s: %w", formatEntryID(p.LeftID), err)
			}
			_, err = db.ExecContext(ctx,
				`INSERT OR IGNORE INTO entry_links (from_id, to_id, relation) VALUES (?, ?, 'superseded_by')`,
				p.LeftID, p.RightID,
			)
			if err != nil {
				return nil, fmt.Errorf("lore: commune: fix link %s: %w", formatEntryID(p.LeftID), err)
			}
			report.FixesApplied = append(report.FixesApplied, CommuneFix{
				Kind:    "reforge",
				EntryID: p.LeftID,
				Detail:  fmt.Sprintf("%s superseded by %s", formatEntryID(p.LeftID), formatEntryID(p.RightID)),
			})
		}
	}

	return report, nil
}

// communeScopeLabel returns the display label for the commune scope.
func communeScopeLabel(allProjects bool) string {
	if allProjects {
		return "(--all-projects)"
	}
	return "(current project)"
}

// communeSeverityIcon picks ✅ / ⚠️ / ❌ based on count and threshold.
func communeSeverityIcon(count, warnThreshold int) string {
	if count == 0 {
		return "✅"
	}
	if count <= warnThreshold {
		return "⚠️ "
	}
	return "❌"
}

// formatCommuneLabel is a helper that formats the display-only label for commune output.
// This is kept here (not in CLI) because it's logic-bearing, not purely presentation.
func formatCommuneLabel(allProjects bool) string {
	return strings.TrimSpace(fmt.Sprintf("🌀 lore commune %s", communeScopeLabel(allProjects)))
}
