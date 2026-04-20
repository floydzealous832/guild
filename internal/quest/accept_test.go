package quest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// TestAccept_Happy claims a freshly-posted quest.
func TestAccept_Happy(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "target"})
	got, err := Accept(context.Background(), db, pid, q.ID, "alice")
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if got.Status != StatusInProgress {
		t.Errorf("status = %q, want in_progress", got.Status)
	}
	if got.Owner != "alice" {
		t.Errorf("owner = %q", got.Owner)
	}
	if got.ClaimedAt == nil {
		t.Error("claimed_at nil")
	}
}

// TestAccept_NotFound returns ErrNotFound.
func TestAccept_NotFound(t *testing.T) {
	db, pid := newTestDB(t)
	_, err := Accept(context.Background(), db, pid, "QUEST-999", "alice")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestAccept_AlreadyClaimed returns AlreadyClaimedError with owner set.
func TestAccept_AlreadyClaimed(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "one"})
	if _, err := Accept(context.Background(), db, pid, q.ID, "alice"); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	_, err := Accept(context.Background(), db, pid, q.ID, "bob")
	if !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("err = %v, want ErrAlreadyClaimed", err)
	}
	var ac *AlreadyClaimedError
	if !errors.As(err, &ac) {
		t.Fatalf("errors.As to AlreadyClaimedError failed: %v", err)
	}
	if ac.Owner != "alice" {
		t.Errorf("ac.Owner = %q, want alice", ac.Owner)
	}
	if ac.QuestID != q.ID {
		t.Errorf("ac.QuestID = %q, want %s", ac.QuestID, q.ID)
	}
}

// TestAccept_Race is the QUEST-9 race invariant: two goroutines calling
// Accept(sameQuest) must produce exactly one success + one
// ErrAlreadyClaimed, never two successes. Runs under -race.
//
// We pre-start both goroutines on a barrier and let them sprint at the
// UPDATE statement simultaneously.
func TestAccept_Race(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "contested"})

	const N = 32 // more goroutines = higher chance of exposing a bug
	var (
		wg       sync.WaitGroup
		start    sync.WaitGroup
		succOk   int64
		errClaim int64
		errOther int64
	)
	start.Add(1)
	wg.Add(N)

	for i := 0; i < N; i++ {
		owner := fmt.Sprintf("worker-%d", i)
		go func(owner string) {
			defer wg.Done()
			start.Wait() // release all workers at once
			_, err := Accept(context.Background(), db, pid, q.ID, owner)
			switch {
			case err == nil:
				atomic.AddInt64(&succOk, 1)
			case errors.Is(err, ErrAlreadyClaimed):
				atomic.AddInt64(&errClaim, 1)
			default:
				atomic.AddInt64(&errOther, 1)
				t.Errorf("unexpected err: %v", err)
			}
		}(owner)
	}
	start.Done()
	wg.Wait()

	succ := atomic.LoadInt64(&succOk)
	claim := atomic.LoadInt64(&errClaim)
	other := atomic.LoadInt64(&errOther)

	if succ != 1 {
		t.Errorf("successes = %d, want exactly 1", succ)
	}
	if claim != N-1 {
		t.Errorf("ErrAlreadyClaimed count = %d, want %d", claim, N-1)
	}
	if other != 0 {
		t.Errorf("other errors = %d, want 0", other)
	}
	t.Logf("accept race N=%d: successes=%d ErrAlreadyClaimed=%d other=%d",
		N, succ, claim, other)

	// Final state: exactly one owner, status=in_progress.
	got := mustLoad(t, db, pid, q.ID)
	if got.Status != StatusInProgress {
		t.Errorf("final status = %q, want in_progress", got.Status)
	}
	if got.Owner == "" {
		t.Error("final owner empty")
	}
	t.Logf("final winner: %s", got.Owner)
}

// TestAccept_TrailFailure verifies that a committed CAS claim returns
// success even when the post-claim trail write fails. This is the
// regression test for the "false failure" bug: the claim is durable in
// task_status, so returning an error would cause a retry to see
// ErrAlreadyClaimed from its own prior success.
func TestAccept_TrailFailure(t *testing.T) {
	db, pid := newTestDB(t)
	q := mustPost(t, db, pid, PostParams{Subject: "trail-fails"})

	// Swap in a trail writer that always fails, restoring after the test.
	orig := acceptTrailWriter
	t.Cleanup(func() { acceptTrailWriter = orig })
	acceptTrailWriter = func(_ context.Context, _ *sql.DB, _, _, _, _ string) error {
		return fmt.Errorf("injected trail failure")
	}

	got, err := Accept(context.Background(), db, pid, q.ID, "alice")
	if err != nil {
		t.Fatalf("Accept returned error despite committed claim: %v", err)
	}
	if got.Status != StatusInProgress {
		t.Errorf("status = %q, want in_progress", got.Status)
	}
	if got.Owner != "alice" {
		t.Errorf("owner = %q, want alice", got.Owner)
	}
	if got.ClaimedAt == nil {
		t.Error("claimed_at nil")
	}

	// A second caller must see ErrAlreadyClaimed, not a raw error.
	_, err2 := Accept(context.Background(), db, pid, q.ID, "bob")
	if !errors.Is(err2, ErrAlreadyClaimed) {
		t.Errorf("second accept err = %v, want ErrAlreadyClaimed", err2)
	}
}

// TestAccept_BlockedNotClaimable verifies a blocked quest can't be
// accepted.
func TestAccept_BlockedNotClaimable(t *testing.T) {
	db, pid := newTestDB(t)
	a := mustPost(t, db, pid, PostParams{Subject: "A"})
	b := mustPost(t, db, pid, PostParams{Subject: "B", DependsOn: []string{a.ID}})
	if b.Status != StatusBlocked {
		t.Fatalf("precondition: b.Status = %s", b.Status)
	}
	_, err := Accept(context.Background(), db, pid, b.ID, "alice")
	if !errors.Is(err, ErrAlreadyClaimed) {
		t.Errorf("err = %v, want ErrAlreadyClaimed for blocked quest", err)
	}
}
