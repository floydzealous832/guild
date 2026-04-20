//go:build audit

// Package mcp — doc-coverage audit utility.
//
// This file is guarded by the "audit" build tag so it NEVER runs in CI.
// Run it manually when you want a full table of coverage locations:
//
//	go test -tags audit -run TestDocCoverageAudit ./internal/mcp/ -v
//
// Output: one row per tool showing match status, match form, line number,
// and the matching snippet. Copy-paste the table into the follow-up quest
// description so the coverage-fill engineer knows exactly which tools
// need examples and where to look in INSTRUCTIONS.
//
// The audit binary uses the REAL staticInstructions string (not a synthetic
// one), so it directly answers "which tools currently have examples in
// the shipped instructions.md?".

package mcp

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// TestDocCoverageAudit prints a full coverage table for staticInstructions.
// Run with: go test -tags audit -run TestDocCoverageAudit ./internal/mcp/ -v
//
// Output format (tab-separated for easy shell parsing):
//
//	STATUS    TOOL                       FORM           LINE  SNIPPET
//	COVERED   guild_archive              invocation       89  guild_archive(project="my-project")
//	MISSING   lore_catalog               -                 -  -
//	EXEMPT    some_tool                  -                 -  reason: ...
func TestDocCoverageAudit(t *testing.T) {
	// Build exemption lookup.
	exempt := make(map[string]string, len(docCoverageExemptions))
	for _, e := range docCoverageExemptions {
		exempt[e.name] = e.rationale
	}

	type auditRow struct {
		tool    string
		status  string
		form    string
		line    int
		snippet string
		note    string
	}

	rows := make([]auditRow, 0, len(expectedTools))
	var coveredCount, missingCount, exemptCount int

	for _, e := range expectedTools {
		row := auditRow{tool: e.name}

		if why, ok := exempt[e.name]; ok {
			row.status = "EXEMPT"
			row.form = "-"
			row.line = 0
			row.snippet = "-"
			row.note = "reason: " + why
			exemptCount++
		} else {
			r := scanDocCoverage(e.name, staticInstructions)
			if r.covered {
				row.status = "COVERED"
				row.form = r.matchForm
				row.line = r.matchLine
				row.snippet = r.matchSnip
				coveredCount++
			} else {
				row.status = "MISSING"
				row.form = "-"
				row.line = 0
				row.snippet = "-"
				missingCount++
			}
		}
		rows = append(rows, row)
	}

	// Sort for stable output.
	sort.Slice(rows, func(i, j int) bool { return rows[i].tool < rows[j].tool })

	// Print header.
	t.Logf("")
	t.Logf("=== staticInstructions doc-coverage audit ===")
	t.Logf("")
	t.Logf("%-8s  %-26s  %-14s  %4s  %s", "STATUS", "TOOL", "FORM", "LINE", "SNIPPET")
	t.Logf("%s", strings.Repeat("-", 90))

	for _, r := range rows {
		lineStr := "-"
		if r.line > 0 {
			lineStr = fmt.Sprintf("%4d", r.line)
		}
		snippet := r.snippet
		if r.note != "" {
			snippet = r.note
		}
		t.Logf("%-8s  %-26s  %-14s  %4s  %s", r.status, r.tool, r.form, lineStr, snippet)
	}

	t.Logf("%s", strings.Repeat("-", 90))

	total := len(expectedTools)
	denominator := total - exemptCount
	pct := 0.0
	if denominator > 0 {
		pct = float64(coveredCount) / float64(denominator) * 100
	}
	t.Logf("")
	t.Logf("Summary: %d/%d tools covered (%.0f%%)  |  %d exempt  |  %d missing",
		coveredCount, denominator, pct, exemptCount, missingCount)
	t.Logf("")

	if missingCount > 0 {
		// Collect suspect list for the quest worklist.
		var suspects []string
		for _, r := range rows {
			if r.status == "MISSING" {
				suspects = append(suspects, r.tool)
			}
		}
		sort.Strings(suspects)
		t.Logf("Worklist for coverage-fill quest:")
		for _, s := range suspects {
			t.Logf("  [ ] %s", s)
		}
	}

	// Audit never FAILS (it's a reporting tool, not a gate). The gate
	// lives in TestTools_DocCoverage.
}
