package telemetry_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mathomhaus/guild/internal/telemetry"
)

// ---- ParseUsageLog -----------------------------------------------------------

// TestParseUsageLog_SixCol verifies that 6-column rows parse all fields.
func TestParseUsageLog_SixCol(t *testing.T) {
	input := "2026-04-19T10:00:00Z\tguild\tguild_session_start\t0\t150\t2048\n"
	rows, err := telemetry.ParseUsageLog(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseUsageLog: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.Tool != "guild_session_start" {
		t.Errorf("Tool: got %q, want %q", r.Tool, "guild_session_start")
	}
	if r.RespBytes != 2048 {
		t.Errorf("RespBytes: got %d, want 2048", r.RespBytes)
	}
	if r.Project != "guild" {
		t.Errorf("Project: got %q, want %q", r.Project, "guild")
	}
}

// TestParseUsageLog_FiveCol verifies that legacy 5-column rows parse with
// RespBytes defaulting to 0 (forward-compat for old log files).
func TestParseUsageLog_FiveCol(t *testing.T) {
	input := "2026-04-19T09:00:00Z\tguild\tlore_appraise\t0\t42\n"
	rows, err := telemetry.ParseUsageLog(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseUsageLog: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].RespBytes != 0 {
		t.Errorf("RespBytes for 5-col row: got %d, want 0", rows[0].RespBytes)
	}
	if rows[0].Tool != "lore_appraise" {
		t.Errorf("Tool: got %q", rows[0].Tool)
	}
}

// TestParseUsageLog_Mixed verifies parsing of mixed 5-col and 6-col rows.
func TestParseUsageLog_Mixed(t *testing.T) {
	input := "" +
		"2026-04-19T08:00:00Z\tproj\ttool_a\t0\t10\n" +
		"2026-04-19T08:01:00Z\tproj\ttool_b\t0\t20\t500\n" +
		"2026-04-19T08:02:00Z\tproj\ttool_c\t1\t5\t0\n"
	rows, err := telemetry.ParseUsageLog(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseUsageLog: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	if rows[0].RespBytes != 0 {
		t.Errorf("row0 RespBytes: got %d, want 0", rows[0].RespBytes)
	}
	if rows[1].RespBytes != 500 {
		t.Errorf("row1 RespBytes: got %d, want 500", rows[1].RespBytes)
	}
	if rows[2].ExitCode != 1 {
		t.Errorf("row2 ExitCode: got %d, want 1", rows[2].ExitCode)
	}
}

// TestParseUsageLog_SkipsMalformed verifies that short/invalid rows are skipped.
func TestParseUsageLog_SkipsMalformed(t *testing.T) {
	input := "" +
		"not-a-timestamp\tproj\ttool\t0\t10\t100\n" +
		"2026-04-19T08:00:00Z\tproj\ttool\t0\t10\t200\n" +
		"toofew\n"
	rows, err := telemetry.ParseUsageLog(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseUsageLog: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 (malformed rows skipped)", len(rows))
	}
	if rows[0].RespBytes != 200 {
		t.Errorf("RespBytes: got %d, want 200", rows[0].RespBytes)
	}
}

// ---- Token math --------------------------------------------------------------

// TestToolStats_EstTokens verifies 4-chars-per-token heuristic.
func TestToolStats_EstTokens(t *testing.T) {
	input := "2026-04-19T10:00:00Z\tguild\ttool_x\t0\t50\t800\n"
	rows, _ := telemetry.ParseUsageLog(strings.NewReader(input))
	report := telemetry.Analyze(rows, "")
	s := report.ByTool["tool_x"]
	if s == nil {
		t.Fatal("tool_x missing from report")
	}
	// 800 bytes / 4 = 200 tokens.
	if s.EstTokens() != 200 {
		t.Errorf("EstTokens: got %d, want 200", s.EstTokens())
	}
	if s.MeanTokens() != 200 {
		t.Errorf("MeanTokens: got %d, want 200", s.MeanTokens())
	}
}

// TestToolStats_EstTokens_Ceiling verifies ceiling division (not floor).
func TestToolStats_EstTokens_Ceiling(t *testing.T) {
	// 5 bytes → ceil(5/4) = 2 tokens.
	input := "2026-04-19T10:00:00Z\tguild\ttool_y\t0\t10\t5\n"
	rows, _ := telemetry.ParseUsageLog(strings.NewReader(input))
	report := telemetry.Analyze(rows, "")
	s := report.ByTool["tool_y"]
	if s == nil {
		t.Fatal("tool_y missing from report")
	}
	if s.EstTokens() != 2 {
		t.Errorf("EstTokens(5 bytes): got %d, want 2", s.EstTokens())
	}
}

// ---- Per-tool aggregation ----------------------------------------------------

// TestAnalyze_PerTool verifies multi-call aggregation for the same tool.
func TestAnalyze_PerTool(t *testing.T) {
	input := "" +
		"2026-04-19T10:00:00Z\tguild\tlore_appraise\t0\t20\t400\n" +
		"2026-04-19T10:00:30Z\tguild\tlore_appraise\t0\t30\t600\n" +
		"2026-04-19T10:00:45Z\tguild\tquest_post\t0\t25\t200\n"
	rows, _ := telemetry.ParseUsageLog(strings.NewReader(input))
	report := telemetry.Analyze(rows, "")

	la := report.ByTool["lore_appraise"]
	if la == nil {
		t.Fatal("lore_appraise missing")
	}
	if la.Calls != 2 {
		t.Errorf("lore_appraise Calls: got %d, want 2", la.Calls)
	}
	if la.TotalBytes != 1000 {
		t.Errorf("lore_appraise TotalBytes: got %d, want 1000", la.TotalBytes)
	}
	// 1000 / 4 = 250 tokens.
	if la.EstTokens() != 250 {
		t.Errorf("lore_appraise EstTokens: got %d, want 250", la.EstTokens())
	}
	// mean = 250 / 2 = 125.
	if la.MeanTokens() != 125 {
		t.Errorf("lore_appraise MeanTokens: got %d, want 125", la.MeanTokens())
	}
}

// ---- Session filtering -------------------------------------------------------

// TestAnalyze_SessionFilter verifies that a session key narrows results.
func TestAnalyze_SessionFilter(t *testing.T) {
	// Two different minutes → two separate session buckets.
	input := "" +
		"2026-04-19T10:00:00Z\tguild\ttool_a\t0\t10\t800\n" +
		"2026-04-19T10:00:30Z\tguild\ttool_b\t0\t15\t400\n" +
		"2026-04-19T10:01:00Z\tguild\ttool_c\t0\t20\t1200\n"
	rows, _ := telemetry.ParseUsageLog(strings.NewReader(input))

	report := telemetry.Analyze(rows, "guild@2026-04-19T10:00")
	if len(report.Sessions) != 1 {
		t.Fatalf("filtered sessions: got %d, want 1", len(report.Sessions))
	}
	if report.Sessions[0].Calls != 2 {
		t.Errorf("session calls: got %d, want 2", report.Sessions[0].Calls)
	}
	if report.Sessions[0].TotalBytes != 1200 {
		t.Errorf("session TotalBytes: got %d, want 1200", report.Sessions[0].TotalBytes)
	}
	// Only tools from that session are in ByTool.
	if _, ok := report.ByTool["tool_c"]; ok {
		t.Errorf("tool_c should be filtered out")
	}
}

// TestMostRecentSession verifies that the latest row's session key is returned.
func TestMostRecentSession(t *testing.T) {
	input := "" +
		"2026-04-19T09:00:00Z\tguild\ttool_a\t0\t10\t100\n" +
		"2026-04-19T10:05:00Z\tguild\ttool_b\t0\t20\t200\n"
	rows, _ := telemetry.ParseUsageLog(strings.NewReader(input))
	key := telemetry.MostRecentSession(rows)
	if key != "guild@2026-04-19T10:05" {
		t.Errorf("MostRecentSession: got %q, want %q", key, "guild@2026-04-19T10:05")
	}
}

// TestMostRecentSession_Empty verifies empty input returns empty string.
func TestMostRecentSession_Empty(t *testing.T) {
	key := telemetry.MostRecentSession(nil)
	if key != "" {
		t.Errorf("MostRecentSession(nil): got %q, want empty", key)
	}
}

// ---- Integration: Record → ParseUsageLog ------------------------------------

// TestRecord_RespBytesRoundTrip writes a row via telemetry.Record and reads
// it back to confirm resp_bytes is preserved end-to-end.
func TestRecord_RespBytesRoundTrip(t *testing.T) {
	home := tempHome(t)
	_ = home

	const respBytes uint = 3721
	err := telemetry.Record(
		context.Background(),
		enabledCfg(),
		"guild", "guild_session_start",
		0, 120*time.Millisecond,
		respBytes,
	)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	logPath, _ := telemetry.UsageLogPath()
	rows, err := telemetry.ParseUsageLogFile(logPath)
	if err != nil {
		t.Fatalf("ParseUsageLogFile: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].RespBytes != respBytes {
		t.Errorf("RespBytes round-trip: got %d, want %d", rows[0].RespBytes, respBytes)
	}
	if rows[0].Tool != "guild_session_start" {
		t.Errorf("Tool: got %q", rows[0].Tool)
	}
}
