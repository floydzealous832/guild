package quest

import (
	"context"
	"testing"
	"time"
)

func TestParseWindow(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"7d", 7 * 24 * time.Hour, false},
		{"2w", 14 * 24 * time.Hour, false},
		{"1m", 30 * 24 * time.Hour, false},
		{"30", 30 * 24 * time.Hour, false},
		{"", 30 * 24 * time.Hour, false},
		{"0d", 0, true},
		{"bad", 0, true},
	}
	for _, tc := range tests {
		d, err := ParseWindow(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseWindow(%q): want error, got nil", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseWindow(%q): %v", tc.input, err)
			continue
		}
		if d != tc.want {
			t.Errorf("ParseWindow(%q) = %v, want %v", tc.input, d, tc.want)
		}
	}
}

func TestPulse_Empty(t *testing.T) {
	db, pid := newTestDB(t)

	r, err := Pulse(context.Background(), db, pid, 0)
	if err != nil {
		t.Fatalf("Pulse: %v", err)
	}
	if r.ClearedTotal != 0 {
		t.Errorf("ClearedTotal = %d, want 0", r.ClearedTotal)
	}
}

func TestPulse_ReworkRate(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	// Post, accept, and clear one regular quest.
	q1 := mustPost(t, db, pid, PostParams{Subject: "regular"})
	if _, err := Accept(ctx, db, pid, q1.ID, "agent"); err != nil {
		t.Fatalf("accept1: %v", err)
	}
	if _, err := Clear(ctx, db, pid, q1.ID, "done"); err != nil {
		t.Fatalf("clear1: %v", err)
	}

	// Post, accept, and clear a rework quest.
	q2 := mustPost(t, db, pid, PostParams{Subject: "rework", ReworkOf: q1.ID})
	if _, err := Accept(ctx, db, pid, q2.ID, "agent"); err != nil {
		t.Fatalf("accept2: %v", err)
	}
	if _, err := Clear(ctx, db, pid, q2.ID, "reworked"); err != nil {
		t.Fatalf("clear2: %v", err)
	}

	r, err := Pulse(ctx, db, pid, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("Pulse: %v", err)
	}
	if r.ClearedTotal != 2 {
		t.Errorf("ClearedTotal = %d, want 2", r.ClearedTotal)
	}
	if r.ReworkCount != 1 {
		t.Errorf("ReworkCount = %d, want 1", r.ReworkCount)
	}
	if r.ReworkPct != 50 {
		t.Errorf("ReworkPct = %d, want 50", r.ReworkPct)
	}
}

func TestPulse_Window_Excludes_Old(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	// Post and clear a quest (will be "old" relative to 1d window since
	// updated_at is set by the DB to the current time — for the window test
	// we manipulate the updated_at directly to simulate an old quest).
	q := mustPost(t, db, pid, PostParams{Subject: "old task"})
	if _, err := Accept(ctx, db, pid, q.ID, "agent"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, err := Clear(ctx, db, pid, q.ID, "done"); err != nil {
		t.Fatalf("clear: %v", err)
	}

	// Backdate updated_at to 3 days ago so it falls outside a 1d window.
	old := time.Now().UTC().Add(-3 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := db.ExecContext(ctx,
		`UPDATE task_status SET updated_at = ? WHERE project_id = ? AND task_id = ?`,
		old, pid, q.ID,
	); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	r, err := Pulse(ctx, db, pid, 1*24*time.Hour)
	if err != nil {
		t.Fatalf("Pulse: %v", err)
	}
	if r.ClearedTotal != 1 {
		t.Errorf("ClearedTotal = %d, want 1 (all time)", r.ClearedTotal)
	}
	// Window is 1d; old quest is 3d ago → not in window.
	if r.ClearedInWindow != 0 {
		t.Errorf("ClearedInWindow = %d, want 0 (outside 1d window)", r.ClearedInWindow)
	}
}

func TestPulse_HotFiles(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	// Two quests both touching "hot.go".
	q1 := mustPost(t, db, pid, PostParams{Subject: "Q1", Files: []string{"hot.go", "a.go"}})
	q2 := mustPost(t, db, pid, PostParams{Subject: "Q2", Files: []string{"hot.go", "b.go"}})
	q3 := mustPost(t, db, pid, PostParams{Subject: "Q3", Files: []string{"cold.go"}})

	for _, q := range []*Quest{q1, q2, q3} {
		if _, err := Accept(ctx, db, pid, q.ID, "agent"); err != nil {
			t.Fatalf("accept %s: %v", q.ID, err)
		}
		if _, err := Clear(ctx, db, pid, q.ID, "done"); err != nil {
			t.Fatalf("clear %s: %v", q.ID, err)
		}
	}

	r, err := Pulse(ctx, db, pid, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("Pulse: %v", err)
	}
	if len(r.HotFiles) == 0 {
		t.Fatal("want at least one hot file, got none")
	}
	if r.HotFiles[0].File != "hot.go" {
		t.Errorf("HotFiles[0] = %q, want hot.go", r.HotFiles[0].File)
	}
	if r.HotFiles[0].QuestCount != 2 {
		t.Errorf("HotFiles[0].QuestCount = %d, want 2", r.HotFiles[0].QuestCount)
	}
}

func TestPulse_NoReport(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	// Clear a quest with no report.
	q := mustPost(t, db, pid, PostParams{Subject: "no-report"})
	if _, err := Accept(ctx, db, pid, q.ID, "agent"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	// Clear with empty report.
	if _, err := Clear(ctx, db, pid, q.ID, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}

	r, err := Pulse(ctx, db, pid, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("Pulse: %v", err)
	}
	if r.NoReport != 1 {
		t.Errorf("NoReport = %d, want 1", r.NoReport)
	}
}
