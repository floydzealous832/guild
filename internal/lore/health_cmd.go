package lore

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mathomhaus/guild/internal/command"
)

// health_cmd.go hosts the registry specs for lore_inquest, lore_meld,
// and lore_commune — three health/audit verbs with similar shapes.

type InquestInput struct {
	AllProjects bool   `json:"all_projects,omitempty" jsonschema:"scan every project, not just current"`
	Project     string `json:"project,omitempty"`
}

type InquestCmdOutput struct {
	Result *InquestResult `json:"result"`
}

var InquestCommand = &command.Command[InquestInput, InquestCmdOutput]{
	Name:       "lore_inquest",
	CLIPath:    []string{"lore", "inquest"},
	CLIAliases: []string{"audit"},
	Short:      "audit oath wall for >60-word principles",
	Long:       "Audit the oath wall for narrative-bloat principles (>60 words). Demote bloated principles to decisions.",
	Args: []command.ArgSpec{
		{Name: "all_projects", Kind: command.ArgFlag, Type: command.ArgBool, Help: "scan every project"},
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in InquestInput) (InquestCmdOutput, error) {
		var pid string
		if !in.AllProjects {
			resolved, err := d.ResolveProj(ctx, in.Project)
			if err != nil {
				return InquestCmdOutput{}, err
			}
			pid = resolved
		}
		db, err := d.OpenDB(ctx)
		if err != nil {
			return InquestCmdOutput{}, err
		}
		defer func() { _ = db.Close() }()
		res, err := Inquest(ctx, db, pid, in.AllProjects, PrincipleMaxWordsDefault)
		if err != nil {
			return InquestCmdOutput{}, err
		}
		return InquestCmdOutput{Result: res}, nil
	},
	CLIFormat: func(s command.CLISink, o InquestCmdOutput) string { return formatInquest(s, o) },
	MCPFormat: func(s command.MCPSink, o InquestCmdOutput) string { return formatInquest(s, o) },
}

func formatInquest(s lineSink, o InquestCmdOutput) string {
	r := o.Result
	if r == nil || len(r.BloatEntries) == 0 {
		return strings.TrimRight(s.Line("⚖️", "[ok]", "no bloated principles — oath wall is healthy"), "\n")
	}
	var b strings.Builder
	b.WriteString(s.Line("⚖️", "[inquest]", fmt.Sprintf("%d bloated principle(s):", len(r.BloatEntries))))
	for _, row := range r.BloatEntries {
		fmt.Fprintf(&b, "  %s [%dw] %s\n", formatEntryID(row.EntryID), row.WordCount, row.Title)
	}
	return strings.TrimRight(b.String(), "\n")
}

type MeldInput struct {
	// Threshold is a string so both CLI (--threshold=0.7) and MCP
	// (JSON numeric/string) coerce through one path. Parsed to float64
	// in the Handler. ArgFloat64 isn't first-class in the registry
	// adapter today (low-demand feature).
	Threshold   string `json:"threshold,omitempty" jsonschema:"Jaccard near-match threshold (e.g. 0.7); default exact match only"`
	AllProjects bool   `json:"all_projects,omitempty" jsonschema:"span every project (default when no project set)"`
	Project     string `json:"project,omitempty"`
}

type MeldCmdOutput struct {
	Pairs []MeldPair `json:"pairs"`
}

var MeldCommand = &command.Command[MeldInput, MeldCmdOutput]{
	Name:       "lore_meld",
	CLIPath:    []string{"lore", "meld"},
	CLIAliases: []string{"dedupe"},
	Short:      "find duplicate lore entries across projects",
	Long:       "Find duplicate lore entries across projects. Surfaces reforge candidates. Cross-project by default.",
	Args: []command.ArgSpec{
		{Name: "threshold", Kind: command.ArgFlag, Type: command.ArgString, Help: "Jaccard near-match threshold (e.g. 0.7); omit for exact-only (default)"},
		{Name: "all_projects", Kind: command.ArgFlag, Type: command.ArgBool, Help: "span every project"},
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in MeldInput) (MeldCmdOutput, error) {
		// Default to exact-only (1.0). Go's zero value for float64 is 0.0,
		// which would pass every entry pair through the O(n²) near-match scan.
		// Callers must opt into fuzzy matching by passing a threshold below 1.0.
		threshold := 1.0
		if s := strings.TrimSpace(in.Threshold); s != "" {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return MeldCmdOutput{}, fmt.Errorf("--threshold %q: %w", in.Threshold, err)
			}
			threshold = v
		}
		pid, err := d.ResolveProj(ctx, in.Project)
		if err != nil {
			return MeldCmdOutput{}, err
		}
		db, err := d.OpenDB(ctx)
		if err != nil {
			return MeldCmdOutput{}, err
		}
		defer func() { _ = db.Close() }()
		allProjects := true
		if in.Project != "" {
			allProjects = false
		}
		if in.AllProjects {
			allProjects = true
		}
		pairs, err := Meld(ctx, db, threshold, allProjects, pid)
		if err != nil {
			return MeldCmdOutput{}, err
		}
		return MeldCmdOutput{Pairs: pairs}, nil
	},
	CLIFormat: func(s command.CLISink, o MeldCmdOutput) string { return formatMeld(s, o) },
	MCPFormat: func(s command.MCPSink, o MeldCmdOutput) string { return formatMeld(s, o) },
}

func formatMeld(s lineSink, o MeldCmdOutput) string {
	if len(o.Pairs) == 0 {
		return strings.TrimRight(s.Line("🪄", "[ok]", "no duplicate pairs — lore is tidy"), "\n")
	}
	var b strings.Builder
	b.WriteString(s.Line("🪄", "[meld]", fmt.Sprintf("%d duplicate pair(s):", len(o.Pairs))))
	for _, p := range o.Pairs {
		fmt.Fprintf(&b, "  %s (%s)  ≈  %s (%s)  (score=%.2f)\n",
			formatEntryID(p.LeftID), p.LeftProject, formatEntryID(p.RightID), p.RightProject, p.Score)
	}
	return strings.TrimRight(b.String(), "\n")
}

type CommuneInput struct {
	AllProjects bool   `json:"all_projects,omitempty" jsonschema:"check across every project"`
	Fix         bool   `json:"fix,omitempty" jsonschema:"apply safe auto-remediation"`
	Project     string `json:"project,omitempty"`
}

type CommuneCmdOutput struct {
	Report *CommuneReport `json:"report"`
}

var CommuneCommand = &command.Command[CommuneInput, CommuneCmdOutput]{
	Name:       "lore_commune",
	CLIPath:    []string{"lore", "commune"},
	CLIAliases: []string{"lint"},
	Short:      "health report for oath bloat and duplicate lore",
	Long:       "Health report for oath bloat and duplicate lore. fix=true auto-remediates severe issues.",
	Args: []command.ArgSpec{
		{Name: "all_projects", Kind: command.ArgFlag, Type: command.ArgBool, Help: "check across every project"},
		{Name: "fix", Kind: command.ArgFlag, Type: command.ArgBool, Help: "apply safe auto-remediation"},
		{Name: "project", Short: "p", Kind: command.ArgFlag, Type: command.ArgString, Help: "project override"},
	},
	Handler: func(ctx context.Context, d command.Deps, in CommuneInput) (CommuneCmdOutput, error) {
		var pid string
		if !in.AllProjects {
			resolved, err := d.ResolveProj(ctx, in.Project)
			if err != nil {
				return CommuneCmdOutput{}, err
			}
			pid = resolved
		}
		db, err := d.OpenDB(ctx)
		if err != nil {
			return CommuneCmdOutput{}, err
		}
		defer func() { _ = db.Close() }()
		rep, err := Commune(ctx, db, pid, in.AllProjects, in.Fix,
			PrincipleMaxWordsDefault, 2*PrincipleMaxWordsDefault)
		if err != nil {
			return CommuneCmdOutput{}, err
		}
		return CommuneCmdOutput{Report: rep}, nil
	},
	CLIFormat: func(s command.CLISink, o CommuneCmdOutput) string { return formatCommune(s, o) },
	MCPFormat: func(s command.MCPSink, o CommuneCmdOutput) string { return formatCommune(s, o) },
}

func formatCommune(s lineSink, o CommuneCmdOutput) string {
	r := o.Report
	if r == nil {
		return strings.TrimRight(s.Line("🌀", "[commune]", "commune report empty"), "\n")
	}
	var b strings.Builder
	b.WriteString(s.Line("🌀", "[commune]",
		fmt.Sprintf("oath bloat: %d (severe: %d) · dup pairs: %d · recall misses: %d/%d",
			r.BloatCount, r.SevereCount, r.DupPairCount, r.RecallMisses, r.RecallSampleSize)))
	if len(r.FixesApplied) > 0 {
		fmt.Fprintf(&b, "  auto-applied %d fix(es)\n", len(r.FixesApplied))
	}
	return strings.TrimRight(b.String(), "\n")
}
