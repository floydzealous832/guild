package quest

import (
	"context"
	"testing"
)

func TestOrders_ReturnsAssigned(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	q1 := mustPost(t, db, pid, PostParams{Subject: "quest 1"})
	q2 := mustPost(t, db, pid, PostParams{Subject: "quest 2"})
	_ = mustPost(t, db, pid, PostParams{Subject: "quest 3"}) // stays next

	// Accept q1 and q2 as agentA.
	if _, err := Accept(ctx, db, pid, q1.ID, "agentA"); err != nil {
		t.Fatalf("Accept q1: %v", err)
	}
	if _, err := Accept(ctx, db, pid, q2.ID, "agentA"); err != nil {
		t.Fatalf("Accept q2: %v", err)
	}

	orders, err := Orders(ctx, db, pid, "agentA")
	if err != nil {
		t.Fatalf("Orders: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("expected 2 orders for agentA, got %d", len(orders))
	}

	ids := map[string]bool{q1.ID: false, q2.ID: false}
	for _, o := range orders {
		if _, ok := ids[o.ID]; ok {
			ids[o.ID] = true
		}
	}
	for id, found := range ids {
		if !found {
			t.Errorf("quest %s not found in orders", id)
		}
	}
}

func TestOrders_Empty(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	orders, err := Orders(ctx, db, pid, "nobody")
	if err != nil {
		t.Fatalf("Orders: %v", err)
	}
	if len(orders) != 0 {
		t.Errorf("expected 0 orders, got %d", len(orders))
	}
}

func TestOrders_IsolatedByAgent(t *testing.T) {
	db, pid := newTestDB(t)
	ctx := context.Background()

	q1 := mustPost(t, db, pid, PostParams{Subject: "for agentA"})
	q2 := mustPost(t, db, pid, PostParams{Subject: "for agentB"})

	if _, err := Accept(ctx, db, pid, q1.ID, "agentA"); err != nil {
		t.Fatalf("Accept q1: %v", err)
	}
	if _, err := Accept(ctx, db, pid, q2.ID, "agentB"); err != nil {
		t.Fatalf("Accept q2: %v", err)
	}

	ordersA, err := Orders(ctx, db, pid, "agentA")
	if err != nil {
		t.Fatalf("Orders agentA: %v", err)
	}
	if len(ordersA) != 1 || ordersA[0].ID != q1.ID {
		t.Errorf("agentA should have exactly q1; got %+v", ordersA)
	}

	ordersB, err := Orders(ctx, db, pid, "agentB")
	if err != nil {
		t.Fatalf("Orders agentB: %v", err)
	}
	if len(ordersB) != 1 || ordersB[0].ID != q2.ID {
		t.Errorf("agentB should have exactly q2; got %+v", ordersB)
	}
}
