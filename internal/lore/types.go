// Package lore implements the lore (knowledge lifecycle) domain: entries,
// entry links, cross-project dedup, BM25+recency+title-boost appraisal, oath
// loading, and health/dedup operations.
//
// This file carries only shared domain types. Both the write surface
// (inscribe/update/seal/link/reforge) and read surface (appraise/study/oath/...)
// import from here so type definitions stay in sync.
// Behavioural code lives in dedicated files (inscribe.go, appraise.go, ...).
package lore

import "time"

// Kind is the entry classification. Enforced as a string enum at API
// boundaries; the DB stores the raw string per 001_init.up.sql.
type Kind string

const (
	KindIdea        Kind = "idea"
	KindResearch    Kind = "research"
	KindDecision    Kind = "decision"
	KindObservation Kind = "observation"
	KindPrinciple   Kind = "principle"
)

// AllKinds returns the valid kinds in display order.
func AllKinds() []Kind {
	return []Kind{KindIdea, KindResearch, KindDecision, KindObservation, KindPrinciple}
}

// Status tracks an entry's lifecycle state. The full vocabulary is defined
// in 001_init.up.sql. Most entries are "current"; agents rarely author the
// others directly.
type Status string

const (
	StatusCurrent    Status = "current"
	StatusStale      Status = "stale"
	StatusSuperseded Status = "superseded"
	StatusArchived   Status = "archived"
	StatusImported   Status = "imported"
	StatusSeed       Status = "seed"
	StatusExploring  Status = "exploring"
	StatusPromoted   Status = "promoted"
	StatusParked     Status = "parked"
)

// Relation labels an entry_links row. Values are defined in 001_init.up.sql.
type Relation string

const (
	RelationInforms     Relation = "informs"
	RelationSupersedes  Relation = "supersedes"
	RelationContradicts Relation = "contradicts"
)

// Entry is the full row shape from the `entries` table. Nullable SQLite
// columns map to pointer / zero-value semantics documented on each field.
type Entry struct {
	ID             int64
	ProjectID      string
	Topic          string
	Kind           Kind
	Title          string
	Summary        string
	Tags           []string // parsed from comma-separated DB column; empty slice if NULL
	FilePath       string   // "" if NULL
	Source         string   // "" if NULL
	Status         Status
	ValidDays      *int // nil means "never stales"
	NeedsReview    bool
	PromptedBy     string // quest_id that triggered this entry; "" if NULL
	CreatedAt      time.Time
	UpdatedAt      time.Time
	AccessCount    int
	LastAccessedAt *time.Time // nil if never accessed
}

// Link is one row of the entry_links table.
type Link struct {
	FromID    int64
	ToID      int64
	Relation  Relation
	CreatedAt time.Time
}

// EntryID is the human-facing form ("LORE-NNN") used in CLI output and
// between-tool references. Helper for stringification.
func EntryID(id int64) string {
	return formatEntryID(id)
}
