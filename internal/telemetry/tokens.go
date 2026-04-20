package telemetry

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// charsPerToken is the byte-to-token conversion heuristic: 4 chars ≈ 1 token.
// Pessimistic for code/schema-heavy responses, adequate for prose.
const charsPerToken = 4

// UsageRow holds one parsed row from usage.log. Both 5-col (legacy) and
// 6-col (current) rows are accepted; missing resp_bytes defaults to 0.
type UsageRow struct {
	Timestamp  time.Time
	Project    string
	Tool       string
	ExitCode   int
	DurationMs int64
	RespBytes  uint
}

// TokenReport aggregates per-tool and per-session token estimates.
type TokenReport struct {
	// ByTool maps tool name → aggregated stats.
	ByTool map[string]*ToolStats
	// Sessions lists per-session aggregates, sorted by session start time.
	Sessions []SessionStats
}

// ToolStats holds aggregated call metrics for one tool.
type ToolStats struct {
	Tool       string
	Calls      int
	TotalBytes uint
}

// EstTokens returns the estimated token count at 4 chars/token.
func (s *ToolStats) EstTokens() uint { return (s.TotalBytes + charsPerToken - 1) / charsPerToken }

// MeanTokens returns average tokens per call (0 when Calls==0).
func (s *ToolStats) MeanTokens() uint {
	if s.Calls <= 0 {
		return 0
	}
	return s.EstTokens() / uint(s.Calls) //nolint:gosec // Calls > 0 guarded above
}

// SessionStats holds aggregate metrics for one session (process invocation).
type SessionStats struct {
	// SessionKey is the minute-granularity bucket used to group tool calls
	// into a logical "session". We group by (project, minute) because usage.log
	// has no explicit session ID.
	SessionKey  string
	Project     string
	Start       time.Time
	Calls       int
	TotalBytes  uint
	TotalTokens uint
}

// ParseUsageLog reads usage.log rows from r. 5-col (legacy) and 6-col rows
// are both accepted; unknown extra columns beyond 6 are ignored.
func ParseUsageLog(r io.Reader) ([]UsageRow, error) {
	var rows []UsageRow
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 5 {
			continue // skip malformed rows
		}
		ts, err := time.Parse(time.RFC3339, fields[0])
		if err != nil {
			continue // skip unparseable timestamps
		}
		exitCode, _ := strconv.Atoi(fields[3])
		durMs, _ := strconv.ParseInt(fields[4], 10, 64)

		var respBytes uint
		if len(fields) >= 6 {
			v, _ := strconv.ParseUint(fields[5], 10, 64)
			respBytes = uint(v)
		}

		rows = append(rows, UsageRow{
			Timestamp:  ts,
			Project:    fields[1],
			Tool:       fields[2],
			ExitCode:   exitCode,
			DurationMs: durMs,
			RespBytes:  respBytes,
		})
	}
	return rows, sc.Err()
}

// ParseUsageLogFile opens path and calls ParseUsageLog.
func ParseUsageLogFile(path string) ([]UsageRow, error) {
	f, err := os.Open(path) //nolint:gosec // caller-controlled path
	if err != nil {
		return nil, fmt.Errorf("tokens: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return ParseUsageLog(f)
}

// Analyze aggregates rows into a TokenReport. If sessionFilter is non-empty
// it must match a SessionStats.SessionKey; only matching rows are included.
func Analyze(rows []UsageRow, sessionFilter string) *TokenReport {
	byTool := make(map[string]*ToolStats)

	// Group calls into minute-granularity session buckets: (project, minute).
	type sessionKey struct{ project, minute string }
	sessMap := make(map[sessionKey]*SessionStats)

	for _, r := range rows {
		minute := r.Timestamp.UTC().Format("2006-01-02T15:04")
		sk := sessionKey{project: r.Project, minute: minute}
		sess := sessMap[sk]
		if sess == nil {
			key := fmt.Sprintf("%s@%s", r.Project, minute)
			sess = &SessionStats{
				SessionKey: key,
				Project:    r.Project,
				Start:      r.Timestamp,
			}
			sessMap[sk] = sess
		}
		if sessionFilter != "" && sess.SessionKey != sessionFilter {
			continue
		}
		sess.Calls++
		sess.TotalBytes += r.RespBytes
		sess.TotalTokens = (sess.TotalBytes + charsPerToken - 1) / charsPerToken

		ts := byTool[r.Tool]
		if ts == nil {
			ts = &ToolStats{Tool: r.Tool}
			byTool[r.Tool] = ts
		}
		ts.Calls++
		ts.TotalBytes += r.RespBytes
	}

	// Build sorted session list.
	var sessions []SessionStats
	for _, s := range sessMap {
		if sessionFilter != "" && s.SessionKey != sessionFilter {
			continue
		}
		sessions = append(sessions, *s)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Start.Before(sessions[j].Start)
	})

	return &TokenReport{ByTool: byTool, Sessions: sessions}
}

// MostRecentSession returns the SessionKey of the last session in rows, or
// empty string if rows is empty.
func MostRecentSession(rows []UsageRow) string {
	if len(rows) == 0 {
		return ""
	}
	// rows may be unordered; find the latest timestamp.
	latest := rows[0]
	for _, r := range rows[1:] {
		if r.Timestamp.After(latest.Timestamp) {
			latest = r
		}
	}
	minute := latest.Timestamp.UTC().Format("2006-01-02T15:04")
	return fmt.Sprintf("%s@%s", latest.Project, minute)
}
