package mcp

// doc-coverage gate — every registered tool MUST have an inline
// example invocation in the staticInstructions constant (instructions.md).
//
// Rationale: tools without example invocations are used less often, since
// agents rely on shown syntax to pick the right tool. This test closes the
// gap permanently at the build layer so the problem cannot regress.
//
// Pattern recognition (three forms are accepted as "covered"):
//
//  1. Invocation form — `tool_name(` anywhere in INSTRUCTIONS, with a
//     word-boundary guard (`\btool_name\(`) to prevent partial-substring
//     false positives.
//
//  2. Named-example form — `Example: tool_name` (case-insensitive
//     prefix) anywhere in INSTRUCTIONS.
//
//  3. Fenced-code invocation — `tool_name(` inside a fenced code block
//     (``` ... ```). Detected by the same word-boundary regex applied to
//     the whole string (form 1 already covers this).
//
// Exemption mechanism:
//
//	docCoverageExemptions is a package-level slice of exemption records.
//	If a tool genuinely needs no example (edge case: a pure-internal or
//	infrastructure tool where an example would be misleading), add an
//	entry with a rationale comment. The test skips exempted tools and
//	logs a visible warning so reviewers see the waiver.
//
//	Example of adding an exemption:
//
//	  docCoverageExemptions = []docExemption{
//	      {
//	          // guild_session_start is the mandatory first call; its
//	          // invocation is already shown in the MANDATORY FIRST STEP
//	          // section header, not as a tool example. Exempt because the
//	          // section-header call IS the example.
//	          name:     "guild_session_start",
//	          rationale: "invocation shown in MANDATORY FIRST STEP header",
//	      },
//	  }
//
//	The exemption list MUST be kept short. Reviewers should treat every
//	addition with the same scrutiny as a security exception.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// docExemption records one tool that is intentionally exempt from the
// doc-coverage gate. The rationale field is required (the linter/reviewer
// will ask for it).
type docExemption struct {
	name      string
	rationale string
}

// docCoverageExemptions is the explicit opt-out registry. Keep this
// slice empty unless a tool genuinely cannot be shown with a safe
// inline example in INSTRUCTIONS.
var docCoverageExemptions = []docExemption{}

// coverageResult holds the per-tool match outcome for a single scan.
type coverageResult struct {
	toolName  string
	covered   bool
	matchForm string // "invocation", "named-example", or ""
	matchLine int    // 1-based line number of first match; 0 if none
	matchSnip string // up to 80 chars of the matching line
	exempted  bool
	exemptWhy string
}

// scanDocCoverage checks whether instructions contains at least one
// acceptable example invocation for toolName. Returns the result.
//
// Two regex forms are tried in priority order:
//  1. word-boundary invocation:  \btool_name\(
//  2. named-example prefix:      (?i)example:\s+tool_name
func scanDocCoverage(toolName, instructions string) coverageResult {
	lines := strings.Split(instructions, "\n")

	// Form 1: word-boundary invocation (\btool_name\()
	invRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(toolName) + `\(`)
	for i, line := range lines {
		if invRe.MatchString(line) {
			snip := line
			if len(snip) > 80 {
				snip = snip[:80]
			}
			return coverageResult{
				toolName:  toolName,
				covered:   true,
				matchForm: "invocation",
				matchLine: i + 1,
				matchSnip: strings.TrimSpace(snip),
			}
		}
	}

	// Form 2: named-example prefix (case-insensitive)
	exRe := regexp.MustCompile(`(?i)example:\s+` + regexp.QuoteMeta(toolName))
	for i, line := range lines {
		if exRe.MatchString(line) {
			snip := line
			if len(snip) > 80 {
				snip = snip[:80]
			}
			return coverageResult{
				toolName:  toolName,
				covered:   true,
				matchForm: "named-example",
				matchLine: i + 1,
				matchSnip: strings.TrimSpace(snip),
			}
		}
	}

	return coverageResult{
		toolName: toolName,
		covered:  false,
	}
}

// TestInstructions_ToolReferencesExist prevents prompt drift: every
// MCP-style tool name mentioned in staticInstructions must be a registered
// tool. This catches prose edits that introduce fake names or CLI-only
// aliases like `quest bounties`.
func TestInstructions_ToolReferencesExist(t *testing.T) {
	valid := make(map[string]struct{}, len(expectedTools))
	for _, e := range expectedTools {
		valid[e.name] = struct{}{}
	}

	refRe := regexp.MustCompile("`((?:guild|lore|quest)_[a-z_]+)`|\\b((?:guild|lore|quest)_[a-z_]+)\\(")
	raw := refRe.FindAllStringSubmatch(staticInstructions, -1)
	var refs []string
	for _, m := range raw {
		ref := m[1]
		if ref == "" {
			ref = m[2]
		}
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		t.Fatal("no MCP tool references found in staticInstructions")
	}

	seen := map[string]struct{}{}
	for _, ref := range refs {
		if _, dup := seen[ref]; dup {
			continue
		}
		seen[ref] = struct{}{}
		if _, ok := valid[ref]; !ok {
			t.Errorf("staticInstructions references unknown tool %q", ref)
		}
	}
}

// TestTools_DocCoverage is the doc-coverage build gate: every registered
// tool must have an example invocation in INSTRUCTIONS. The test:
//
//  1. Iterates expectedTools (the authoritative registry in tools_test.go).
//  2. Skips tools in docCoverageExemptions (with a loud log warning).
//  3. For each tool, scans INSTRUCTIONS for an acceptable invocation form.
//  4. Collects the "suspect set" — tools with no example found.
//  5. Logs coverage stats: N/M tools covered + suspect set list.
//  6. FAILS if suspect set is non-empty.
//
// If this test fails because INSTRUCTIONS is missing examples (expected
// in the initial port — only 5/28 tools are covered), do NOT edit
// INSTRUCTIONS to fake examples. Instead:
//   - The test failure IS the signal: file a follow-up quest to add real
//     examples to INSTRUCTIONS.
//   - The suspect list in the failure message is the exact worklist.
func TestTools_DocCoverage(t *testing.T) {
	// Build the exemption lookup for O(1) access.
	exempt := make(map[string]string, len(docCoverageExemptions))
	for _, e := range docCoverageExemptions {
		exempt[e.name] = e.rationale
	}

	// Scan every registered tool.
	results := make([]coverageResult, 0, len(expectedTools))
	for _, e := range expectedTools {
		r := scanDocCoverage(e.name, staticInstructions)
		if why, ok := exempt[e.name]; ok {
			r.exempted = true
			r.exemptWhy = why
		}
		results = append(results, r)
	}

	// Sort for stable log output.
	sort.Slice(results, func(i, j int) bool {
		return results[i].toolName < results[j].toolName
	})

	// Tally.
	var covered, exemptCount int
	var suspects []string
	for _, r := range results {
		switch {
		case r.exempted:
			exemptCount++
			t.Logf("  EXEMPT   %-24s  reason: %s", r.toolName, r.exemptWhy)
		case r.covered:
			covered++
			t.Logf("  COVERED  %-24s  form=%-14s line=%d  %q",
				r.toolName, r.matchForm, r.matchLine, r.matchSnip)
		default:
			suspects = append(suspects, r.toolName)
			t.Logf("  MISSING  %-24s  (no invocation or named-example found)", r.toolName)
		}
	}

	total := len(expectedTools)
	denominator := total - exemptCount
	t.Logf("=== doc-coverage: %d/%d tools have examples (%.0f%%) — %d exempt, %d missing ===",
		covered, denominator, float64(covered)/float64(denominator)*100,
		exemptCount, len(suspects))

	if len(suspects) > 0 {
		sort.Strings(suspects)
		t.Errorf(
			"doc-coverage FAIL: %d/%d tools missing example invocations in INSTRUCTIONS.\n"+
				"Suspect set:\n  %s\n\n"+
				"Action required: file a follow-up quest to add example invocations for each\n"+
				"suspect tool to INSTRUCTIONS. Do NOT add exemptions without a documented\n"+
				"rationale and a follow-up quest.",
			len(suspects), denominator,
			strings.Join(suspects, "\n  "),
		)
	}
}

// TestDocCoverage_AllCoveredPath verifies the scanner correctly identifies
// coverage when every tool has an example. Uses a synthetic INSTRUCTIONS
// string containing exactly the expected invocation forms.
func TestDocCoverage_AllCoveredPath(t *testing.T) {
	// Build a synthetic INSTRUCTIONS string with one invocation per tool.
	var sb strings.Builder
	for _, e := range expectedTools {
		fmt.Fprintf(&sb, "%s(arg=\"val\")\n", e.name)
	}
	synthetic := sb.String()

	exempt := make(map[string]string)
	var suspects []string
	for _, e := range expectedTools {
		r := scanDocCoverage(e.name, synthetic)
		if !r.covered && exempt[e.name] == "" {
			suspects = append(suspects, e.name)
		}
	}
	if len(suspects) > 0 {
		t.Errorf("all-covered synthetic INSTRUCTIONS still has suspects: %v", suspects)
	}
}

// TestDocCoverage_MissingExamplePath verifies the scanner correctly
// surfaces a missing tool in the suspect set when its example is absent.
func TestDocCoverage_MissingExamplePath(t *testing.T) {
	// Build synthetic INSTRUCTIONS with every tool EXCEPT lore_study.
	const absent = "lore_study"
	var sb strings.Builder
	for _, e := range expectedTools {
		if e.name == absent {
			continue
		}
		fmt.Fprintf(&sb, "%s(x=1)\n", e.name)
	}
	synthetic := sb.String()

	r := scanDocCoverage(absent, synthetic)
	if r.covered {
		t.Errorf("expected %s to be missing from suspect set, but scanner reported covered", absent)
	}
	// All others should be covered.
	for _, e := range expectedTools {
		if e.name == absent {
			continue
		}
		r2 := scanDocCoverage(e.name, synthetic)
		if !r2.covered {
			t.Errorf("expected %s to be covered in synthetic INSTRUCTIONS, but scanner reported missing", e.name)
		}
	}
}

// TestDocCoverage_RegexSpecificity verifies that a substring-prefix of a
// tool name does NOT falsely count as coverage for the base tool. For
// example, "quest_lister(" must not match "quest_list".
func TestDocCoverage_RegexSpecificity(t *testing.T) {
	cases := []struct {
		tool      string
		adversary string // adversary string that should NOT match tool
		wantCover bool
	}{
		// "quest_listing_verb(" must not match "quest_list".
		{tool: "quest_list", adversary: "quest_listing_verb(arg=1)", wantCover: false},
		// "lore_appraise_extra(" must not match "lore_appraise".
		{tool: "lore_appraise", adversary: "lore_appraise_extra()", wantCover: false},
		// Exact match SHOULD be covered.
		{tool: "quest_list", adversary: "quest_list(project=\"p\")", wantCover: true},
	}

	for _, c := range cases {
		r := scanDocCoverage(c.tool, c.adversary)
		if r.covered != c.wantCover {
			t.Errorf("tool=%s adversary=%q: covered=%v, want %v",
				c.tool, c.adversary, r.covered, c.wantCover)
		}
	}
}

// TestDocCoverage_ExemptionMechanism verifies that a tool added to
// docCoverageExemptions is skipped (not added to the suspect set) even
// when it has no example in INSTRUCTIONS.
func TestDocCoverage_ExemptionMechanism(t *testing.T) {
	// Use a synthetic INSTRUCTIONS string that covers everything EXCEPT
	// lore_oath. The test verifies that when lore_oath is exempted, the
	// scan doesn't surface it as a failure.
	const exemptTool = "lore_oath"

	// Verify lore_oath is NOT in the real exemptions (we'd be
	// shadowing a real entry; the test is about the mechanism).
	for _, e := range docCoverageExemptions {
		if e.name == exemptTool {
			t.Skipf("lore_oath is already a real exemption; mechanism test uses different tool")
		}
	}

	// Build synthetic INSTRUCTIONS with every tool EXCEPT lore_oath.
	var sb strings.Builder
	for _, e := range expectedTools {
		if e.name == exemptTool {
			continue
		}
		fmt.Fprintf(&sb, "%s()\n", e.name)
	}
	synthetic := sb.String()

	// Scan with lore_oath in the exemption set.
	localExempt := map[string]string{
		exemptTool: "test-only exemption for mechanism verification",
	}

	var suspects []string
	for _, e := range expectedTools {
		if _, ok := localExempt[e.name]; ok {
			// Exempted — must be skipped.
			continue
		}
		r := scanDocCoverage(e.name, synthetic)
		if !r.covered {
			suspects = append(suspects, e.name)
		}
	}

	// Suspect set must be empty because: every non-exempt tool is in
	// synthetic INSTRUCTIONS, and lore_oath is exempted.
	if len(suspects) > 0 {
		t.Errorf("exemption mechanism failed: suspects=%v (lore_oath should be exempt, others should be covered)", suspects)
	}

	// Double-check: without the exemption, lore_oath IS in the suspect set.
	r := scanDocCoverage(exemptTool, synthetic)
	if r.covered {
		t.Errorf("lore_oath should be missing from synthetic INSTRUCTIONS but scanner says covered")
	}
}

// TestDocCoverage_NamedExampleForm verifies that the "Example: tool_name"
// named-example pattern is also accepted as coverage (form 2).
func TestDocCoverage_NamedExampleForm(t *testing.T) {
	cases := []struct {
		name         string
		instructions string
		wantCovered  bool
		wantForm     string
	}{
		{
			name:         "lore_study",
			instructions: "Example: lore_study",
			wantCovered:  true,
			wantForm:     "named-example",
		},
		{
			name:         "lore_study",
			instructions: "EXAMPLE: lore_study with more text",
			wantCovered:  true,
			wantForm:     "named-example",
		},
		{
			// Invocation form takes priority over named-example.
			name:         "lore_study",
			instructions: "lore_study(entry_id=1)",
			wantCovered:  true,
			wantForm:     "invocation",
		},
		{
			// No example at all.
			name:         "lore_study",
			instructions: "Use lore_study to read entries.",
			wantCovered:  false,
		},
	}

	for _, c := range cases {
		r := scanDocCoverage(c.name, c.instructions)
		if r.covered != c.wantCovered {
			t.Errorf("%q instructions=%q: covered=%v want %v",
				c.name, c.instructions, r.covered, c.wantCovered)
			continue
		}
		if c.wantCovered && r.matchForm != c.wantForm {
			t.Errorf("%q instructions=%q: matchForm=%q want %q",
				c.name, c.instructions, r.matchForm, c.wantForm)
		}
	}
}
