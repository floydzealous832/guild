package hints

import (
	"context"
	"testing"
)

// BenchmarkEngine_Evaluate_50EventHistory measures Evaluate latency
// against a pre-populated 50-event Context. QUEST-58 spec targets <5ms
// p99 per call on this fixture — the benchmark establishes a regression
// gate for the hot path.
func BenchmarkEngine_Evaluate_50EventHistory(b *testing.B) {
	store, _ := newTestStore(b)
	eng := NewEngine(store, "bench", EraMCP)
	if err := eng.LoadRules(context.Background()); err != nil {
		b.Fatalf("LoadRules: %v", err)
	}
	// Seed 50 mixed events so rules that iterate history have work.
	eng.Context().RecordEvent(CallEvent{Tool: "guild_session_start"})
	for i := 0; i < 50; i++ {
		tool := "quest_list"
		if i%3 == 0 {
			tool = "lore_appraise"
		}
		eng.Context().RecordEvent(CallEvent{Tool: tool})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eng.Evaluate(context.Background(), CallEvent{
			Tool: "lore_inscribe",
			Args: map[string]any{
				"title":   "topic research",
				"summary": "details",
				"kind":    "research",
			},
		})
	}
}
