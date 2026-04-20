package hints

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrRuleNotFound is returned when a rule_id lookup misses. Callers
// typically reach for errors.Is to distinguish "no row" from "query
// failed" without string-matching.
var ErrRuleNotFound = errors.New("hints: rule not found")

// RuleRow is the DB-backed view of a single row in the `hints` table.
// Rule provides the trigger/follow-through behavior; RuleRow is the
// metadata piece the engine composes with Rule.ID.
type RuleRow struct {
	// ID is the rule_id string.
	ID string
	// TriggerTool is the tool name filter (or "*").
	TriggerTool string
	// Severity is the DB-stored base severity.
	Severity Severity
	// Template is the rendered hint text (with placeholders resolved by
	// Engine before display).
	Template string
	// CooldownCalls is how many calls must pass before the same rule can
	// re-fire.
	CooldownCalls int
	// PerEraSeverity is the raw JSON string from per_era_severity. Empty
	// means "no era override".
	PerEraSeverity string
	// Enabled is the live enabled flag; false means auto-disabled by
	// prune or operator.
	Enabled bool
}

// Store encapsulates the SQL read/write operations the Engine performs
// against the hints + hint_fires tables. Backed by a *sql.DB which the
// caller opens and closes — the Store itself is stateless.
type Store struct {
	// DB is the quest-side database handle. Must point at the DB that
	// ran migration 001_init.up.sql (quest.db in production).
	DB *sql.DB
}

// NewStore returns a Store wrapping db.
func NewStore(db *sql.DB) *Store { return &Store{DB: db} }

// LoadRules reads every row from `hints` into a map keyed by rule_id.
// Returns a zero-length map when the table is empty.
func (s *Store) LoadRules(ctx context.Context) (map[string]RuleRow, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("hints: load rules: nil store/db")
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT rule_id, trigger_tool, severity, template, cooldown_calls,
		       COALESCE(per_era_severity, ''), enabled
		FROM hints`)
	if err != nil {
		return nil, fmt.Errorf("hints: load rules: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]RuleRow{}
	for rows.Next() {
		var (
			rr         RuleRow
			severity   string
			enabledInt int64
			perEra     string
		)
		if err := rows.Scan(&rr.ID, &rr.TriggerTool, &severity, &rr.Template,
			&rr.CooldownCalls, &perEra, &enabledInt); err != nil {
			return nil, fmt.Errorf("hints: load rules: scan: %w", err)
		}
		rr.Severity = ParseSeverity(severity)
		rr.PerEraSeverity = perEra
		rr.Enabled = enabledInt != 0
		out[rr.ID] = rr
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hints: load rules: iterate: %w", err)
	}
	return out, nil
}

// GetRule returns the RuleRow for ruleID or ErrRuleNotFound.
func (s *Store) GetRule(ctx context.Context, ruleID string) (RuleRow, error) {
	if s == nil || s.DB == nil {
		return RuleRow{}, fmt.Errorf("hints: get rule: nil store/db")
	}
	var (
		rr         RuleRow
		severity   string
		enabledInt int64
		perEra     string
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT rule_id, trigger_tool, severity, template, cooldown_calls,
		       COALESCE(per_era_severity, ''), enabled
		FROM hints WHERE rule_id = ?`, ruleID).Scan(
		&rr.ID, &rr.TriggerTool, &severity, &rr.Template,
		&rr.CooldownCalls, &perEra, &enabledInt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RuleRow{}, fmt.Errorf("%w: %s", ErrRuleNotFound, ruleID)
		}
		return RuleRow{}, fmt.Errorf("hints: get rule: query: %w", err)
	}
	rr.Severity = ParseSeverity(severity)
	rr.PerEraSeverity = perEra
	rr.Enabled = enabledInt != 0
	return rr, nil
}

// SetEnabled flips the enabled flag for ruleID. Used by the auto-prune
// pass and by the `guild hints enable|disable` CLI.
func (s *Store) SetEnabled(ctx context.Context, ruleID string, enabled bool) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("hints: set enabled: nil store/db")
	}
	v := int64(0)
	if enabled {
		v = 1
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE hints SET enabled = ? WHERE rule_id = ?`, v, ruleID)
	if err != nil {
		return fmt.Errorf("hints: set enabled: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrRuleNotFound, ruleID)
	}
	return nil
}

// SetSeverity rewrites the severity column for ruleID. Used by the
// auto-prune pass (demote hint → fyi before full disable).
func (s *Store) SetSeverity(ctx context.Context, ruleID string, sev Severity) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("hints: set severity: nil store/db")
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE hints SET severity = ? WHERE rule_id = ?`, sev.String(), ruleID)
	if err != nil {
		return fmt.Errorf("hints: set severity: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrRuleNotFound, ruleID)
	}
	return nil
}

// RecordFire appends one row to hint_fires with followed_through=NULL.
// Returns the inserted row id so the caller can later update the same
// row with a follow-through score.
func (s *Store) RecordFire(ctx context.Context, ruleID, toolCallID, sessionID string, at time.Time) (int64, error) {
	if s == nil || s.DB == nil {
		return 0, fmt.Errorf("hints: record fire: nil store/db")
	}
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO hint_fires (rule_id, tool_call_id, session_id, fired_at)
		VALUES (?, NULLIF(?,''), NULLIF(?,''), ?)`,
		ruleID, toolCallID, sessionID, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("hints: record fire: %w", err)
	}
	return res.LastInsertId()
}

// RecordFollowThrough updates a hint_fires row with the follow-through
// score. offset is how many events passed between fire and hit.
func (s *Store) RecordFollowThrough(ctx context.Context, fireID int64, hit bool, offset int) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("hints: record follow-through: nil store/db")
	}
	v := int64(0)
	if hit {
		v = 1
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE hint_fires
		   SET followed_through = ?, followup_event_offset = ?
		 WHERE id = ?`, v, offset, fireID)
	if err != nil {
		return fmt.Errorf("hints: record follow-through: %w", err)
	}
	return nil
}

// Stats is one rule's aggregate row for the `guild hints stats` CLI and
// for the auto-prune scorer.
type Stats struct {
	// RuleID mirrors the hints.rule_id column.
	RuleID string
	// Severity is the current enabled severity (may differ from the
	// rule's original severity if prune/operator demoted it).
	Severity Severity
	// Enabled is the current enabled flag.
	Enabled bool
	// Fires is the total number of rows in hint_fires for this rule.
	Fires int
	// Scored is the subset of Fires with followed_through IS NOT NULL.
	Scored int
	// Hits is the subset of Scored with followed_through = 1.
	Hits int
}

// HitRate returns Hits/Scored as a float in [0, 1]. Returns 0 when
// Scored is 0 — callers should gate prune decisions on MinScored first.
func (s Stats) HitRate() float64 {
	if s.Scored == 0 {
		return 0
	}
	return float64(s.Hits) / float64(s.Scored)
}

// StatsAll returns per-rule Stats across every row in `hints`, including
// rules that have never fired (Fires/Scored/Hits=0).
func (s *Store) StatsAll(ctx context.Context) ([]Stats, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("hints: stats all: nil store/db")
	}
	// LEFT JOIN so rules with zero fires still get a row. COALESCE the
	// aggregate columns to 0 for that same reason.
	rows, err := s.DB.QueryContext(ctx, `
		SELECT h.rule_id, h.severity, h.enabled,
		       COUNT(f.id)                                        AS fires,
		       COUNT(CASE WHEN f.followed_through IS NOT NULL
		                  THEN 1 END)                             AS scored,
		       COUNT(CASE WHEN f.followed_through = 1
		                  THEN 1 END)                             AS hits
		  FROM hints h
		  LEFT JOIN hint_fires f ON f.rule_id = h.rule_id
		 GROUP BY h.rule_id, h.severity, h.enabled
		 ORDER BY h.rule_id`)
	if err != nil {
		return nil, fmt.Errorf("hints: stats all: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Stats
	for rows.Next() {
		var (
			st         Stats
			sev        string
			enabledInt int64
		)
		if err := rows.Scan(&st.RuleID, &sev, &enabledInt,
			&st.Fires, &st.Scored, &st.Hits); err != nil {
			return nil, fmt.Errorf("hints: stats all: scan: %w", err)
		}
		st.Severity = ParseSeverity(sev)
		st.Enabled = enabledInt != 0
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hints: stats all: iterate: %w", err)
	}
	return out, nil
}

// PendingFire is one unscored hint_fires row awaiting follow-through
// evaluation.
type PendingFire struct {
	// ID is the hint_fires.id used for the scoring UPDATE.
	ID int64
	// RuleID identifies which Rule.FollowThrough detector to run.
	RuleID string
	// SessionID scopes the detector to this session's events only.
	SessionID string
	// FiredAt is the timestamp the fire was recorded at.
	FiredAt time.Time
}

// PendingFires returns every unscored hint_fires row for sessionID.
// sessionID may be "" to match the process-wide set; the prune loop
// uses that form.
func (s *Store) PendingFires(ctx context.Context, sessionID string) ([]PendingFire, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("hints: pending fires: nil store/db")
	}
	query := `SELECT id, rule_id, COALESCE(session_id, ''), fired_at
		FROM hint_fires WHERE followed_through IS NULL`
	args := []any{}
	if sessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	query += ` ORDER BY fired_at ASC`
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("hints: pending fires: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []PendingFire
	for rows.Next() {
		var (
			p       PendingFire
			firedAt string
			sess    string
		)
		if err := rows.Scan(&p.ID, &p.RuleID, &sess, &firedAt); err != nil {
			return nil, fmt.Errorf("hints: pending fires: scan: %w", err)
		}
		p.SessionID = sess
		t, _ := time.Parse(time.RFC3339Nano, firedAt)
		if t.IsZero() {
			t, _ = time.Parse(time.RFC3339, firedAt)
		}
		p.FiredAt = t.UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hints: pending fires: iterate: %w", err)
	}
	return out, nil
}
