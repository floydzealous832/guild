package lore

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// MeldPair is one pair of near-duplicate or exact-duplicate entries across
// (potentially multiple) projects.
type MeldPair struct {
	LeftID       int64
	LeftProject  string
	RightID      int64
	RightProject string
	Score        float64 // 1.0 = exact; 0.0-1.0 = Jaccard near-match
	KindDrift    bool    // true when left and right have different kind values
}

// meldWordRe matches word tokens of ≥3 chars, used for Jaccard similarity.
var meldWordRe = regexp.MustCompile(`\b\w{3,}\b`)

// normalizeForDedup performs aggressive normalization for hash-based exact dup
// detection: lowercase, collapse whitespace, drop non-alphanumeric except spaces.
func normalizeForDedup(s string) string {
	s = strings.ToLower(s)
	// Replace non-word chars with space.
	s = regexp.MustCompile(`[^\w\s]`).ReplaceAllString(s, " ")
	// Collapse multiple whitespace.
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// jaccardSimilarity computes token-set Jaccard similarity between two strings.
// Tokens are runs of ≥3 word characters (letters/digits).
func jaccardSimilarity(a, b string) float64 {
	aTokens := meldWordRe.FindAllString(strings.ToLower(a), -1)
	bTokens := meldWordRe.FindAllString(strings.ToLower(b), -1)

	aSet := make(map[string]struct{}, len(aTokens))
	for _, t := range aTokens {
		aSet[t] = struct{}{}
	}
	bSet := make(map[string]struct{}, len(bTokens))
	for _, t := range bTokens {
		bSet[t] = struct{}{}
	}

	if len(aSet) == 0 && len(bSet) == 0 {
		return 0
	}

	intersection := 0
	for t := range aSet {
		if _, ok := bSet[t]; ok {
			intersection++
		}
	}
	union := len(aSet) + len(bSet) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// meldRow is an internal struct used during Meld processing.
type meldRow struct {
	id        int64
	projectID string
	title     string
	summary   string
	kind      Kind
}

// Meld surfaces duplicate lore entries using a two-pass algorithm:
//
//  1. Exact-match pass: normalize (title+summary), hash each entry; pairs that
//     hash-collide are exact duplicates (score=1.0). This catches the dominant
//     dup class: cross-project rename artifacts where text was copied verbatim.
//
//  2. Near-match pass (only when threshold < 1.0): Jaccard similarity over
//     token sets for pairs not already flagged as exact. O(n²) but stdlib-only.
//
// Cross-project search is the default: project-scoped checks miss a significant
// fraction of duplicates that span project boundaries.
//
// threshold: Jaccard similarity floor for near-match detection.
//   - Pass 1.0 to skip near-match (exact-only mode).
//   - Typical default: 0.9 (--threshold N flag).
//
// Pass threshold=1.0 for exact-only; threshold<1.0 enables near-match.
func Meld(ctx context.Context, db *sql.DB, threshold float64, allProjects bool, projectID string) ([]MeldPair, error) {
	if db == nil {
		return nil, fmt.Errorf("lore: meld: nil db")
	}

	var rows *sql.Rows
	var err error

	if allProjects {
		rows, err = db.QueryContext(ctx,
			`SELECT e.id, p.id AS project_id, e.title, e.summary, e.kind
			   FROM entries e
			   JOIN projects p ON e.project_id = p.id
			  WHERE e.status = 'current'
			  ORDER BY e.id`,
		)
	} else {
		if strings.TrimSpace(projectID) == "" {
			return nil, fmt.Errorf("lore: meld: projectID required when allProjects=false")
		}
		rows, err = db.QueryContext(ctx,
			`SELECT e.id, ? AS project_id, e.title, e.summary, e.kind
			   FROM entries e
			  WHERE e.project_id = ?
			    AND e.status = 'current'
			  ORDER BY e.id`,
			projectID, projectID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("lore: meld: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []meldRow
	for rows.Next() {
		var r meldRow
		var kindStr string
		if err := rows.Scan(&r.id, &r.projectID, &r.title, &r.summary, &kindStr); err != nil {
			return nil, fmt.Errorf("lore: meld: scan: %w", err)
		}
		r.kind = Kind(kindStr)
		entries = append(entries, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lore: meld: iterate: %w", err)
	}

	if len(entries) == 0 {
		return nil, nil
	}

	// Pass 1: hash-based exact duplicate detection.
	byHash := make(map[string][]int) // hash → slice of indices into entries
	for i, e := range entries {
		h := normalizeForDedup(e.title + " " + e.summary)
		byHash[h] = append(byHash[h], i)
	}

	var pairs []MeldPair
	exactPairKeys := make(map[[2]int64]struct{})

	for _, group := range byHash {
		if len(group) < 2 {
			continue
		}
		// Emit unordered pairs within the group (lower id on left).
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				a, b := entries[group[i]], entries[group[j]]
				// Lower id on left to maintain a canonical ordering.
				if a.id > b.id {
					a, b = b, a
				}
				pairs = append(pairs, MeldPair{
					LeftID:       a.id,
					LeftProject:  a.projectID,
					RightID:      b.id,
					RightProject: b.projectID,
					Score:        1.0,
					KindDrift:    a.kind != b.kind,
				})
				exactPairKeys[[2]int64{a.id, b.id}] = struct{}{}
			}
		}
	}

	// Pass 2: Jaccard near-match (only when threshold < 1.0).
	if threshold < 1.0 {
		n := len(entries)
		var nearPairs []MeldPair
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				a, b := entries[i], entries[j]
				key := [2]int64{a.id, b.id}
				if a.id > b.id {
					key = [2]int64{b.id, a.id}
				}
				if _, exact := exactPairKeys[key]; exact {
					continue
				}
				aText := a.title + " " + a.summary
				bText := b.title + " " + b.summary
				score := jaccardSimilarity(aText, bText)
				if score >= threshold {
					left, right := a, b
					if left.id > right.id {
						left, right = right, left
					}
					nearPairs = append(nearPairs, MeldPair{
						LeftID:       left.id,
						LeftProject:  left.projectID,
						RightID:      right.id,
						RightProject: right.projectID,
						Score:        roundFloat(score, 3),
						KindDrift:    left.kind != right.kind,
					})
				}
			}
		}
		// Sort near pairs by score descending.
		sortMeldPairsDesc(nearPairs)
		pairs = append(pairs, nearPairs...)
	}

	return pairs, nil
}

// roundFloat rounds f to decimalPlaces decimal places.
func roundFloat(f float64, places int) float64 {
	shift := 1.0
	for i := 0; i < places; i++ {
		shift *= 10
	}
	if f >= 0 {
		return float64(int(f*shift+0.5)) / shift
	}
	return float64(int(f*shift-0.5)) / shift
}

// sortMeldPairsDesc sorts a MeldPair slice by Score descending in-place.
func sortMeldPairsDesc(pairs []MeldPair) {
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && pairs[j-1].Score < pairs[j].Score; j-- {
			pairs[j-1], pairs[j] = pairs[j], pairs[j-1]
		}
	}
}
