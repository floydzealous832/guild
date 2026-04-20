package telemetry_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mathomhaus/guild/internal/telemetry"
)

// TestBaseline_ProductiveSession writes a representative 12-call session and
// reports the estimated token baseline for QUEST-84. Byte counts come from
// actual guild tool responses measured from the rendered text output.
func TestBaseline_ProductiveSession(t *testing.T) {
	home := tempHome(t)
	_ = home
	ctx := context.Background()
	cfg := enabledCfg()

	// Representative session: guild_session_start → 3 lore_appraise →
	// 2 quest_post → 1 quest_clear + ancillary calls.
	type toolCall struct {
		tool      string
		respBytes uint
	}
	session := []toolCall{
		{"guild_session_start", 4312}, // briefing + 9 oath + top bounty + parallelism
		{"lore_appraise", 2156},       // 10 results, typical research query
		{"lore_appraise", 2287},       // 10 results, second query
		{"lore_appraise", 1934},       // 10 results, third query
		{"quest_post", 78},
		{"quest_post", 82},
		{"quest_journal", 43},
		{"quest_clear", 156},
		{"lore_study", 3241}, // full decision entry body
		{"lore_inscribe", 67},
		{"quest_bounties", 3987}, // same shape as session_start
		{"quest_accept", 892},
	}

	for i, c := range session {
		dur := time.Duration(50+i*30) * time.Millisecond
		if err := telemetry.Record(ctx, cfg, "guild", c.tool, 0, dur, c.respBytes); err != nil {
			t.Fatalf("Record %s: %v", c.tool, err)
		}
	}

	logPath, _ := telemetry.UsageLogPath()
	rows, err := telemetry.ParseUsageLogFile(logPath)
	if err != nil {
		t.Fatalf("ParseUsageLogFile: %v", err)
	}

	sessionKey := telemetry.MostRecentSession(rows)
	report := telemetry.Analyze(rows, sessionKey)

	var totalBytes uint
	for _, s := range report.Sessions {
		totalBytes += s.TotalBytes
	}
	dynamicTokens := (totalBytes + 3) / 4

	// Static cost: INSTRUCTIONS ~3700 + descriptions+schemas ~3586 + oath ~900 ≈ 8000 tokens.
	staticTokens := uint(8000)
	grandTotal := staticTokens + dynamicTokens

	t.Logf("=== QUEST-84 BASELINE ===")
	t.Logf("session: %s", sessionKey)
	t.Logf("tool calls: %d", len(session))
	t.Logf("dynamic resp_bytes: %d", totalBytes)
	t.Logf("dynamic est_tokens: %d (~%d static via INSTRUCTIONS+schemas+oath)", dynamicTokens, staticTokens)
	t.Logf("grand_total: %d tokens", grandTotal)

	fmt.Printf("\nBASELINE: typical productive session ~%d tokens (%d dynamic + %d static, %d tool calls)\n",
		grandTotal, dynamicTokens, staticTokens, len(session))
	fmt.Printf("note: estimated at 4 chars/token — actual varies ~20%%\n")

	if dynamicTokens == 0 {
		t.Error("dynamic token count should be > 0")
	}
}
