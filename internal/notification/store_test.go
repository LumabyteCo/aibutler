package notification_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/notification"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestNotificationStoreLifecycle(t *testing.T) {
	db := testutil.TestDB(t).Conn()
	store := notification.NewStore(db)
	ctx := context.Background()

	if _, err := store.Enqueue(ctx, "webchat", "u1", "  "); err != nil {
		t.Fatalf("empty enqueue: %v", err)
	}
	if n, _ := store.CountPending(ctx, "webchat"); n != 0 {
		t.Errorf("empty text created %d rows", n)
	}

	id, err := store.Enqueue(ctx, "webchat", "u1", "Goodnight needs your PIN")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if id == 0 {
		t.Fatal("expected a row ID")
	}

	pending, err := store.Pending(ctx, "webchat", 10)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].Text != "Goodnight needs your PIN" {
		t.Fatalf("pending = %+v", pending)
	}

	if err := store.MarkDelivered(ctx, id); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	pending, _ = store.Pending(ctx, "webchat", 10)
	if len(pending) != 0 {
		t.Errorf("delivered notification still pending: %+v", pending)
	}
	if n, _ := store.CountPending(ctx, "webchat"); n != 0 {
		t.Errorf("CountPending = %d, want 0", n)
	}
}

func TestNotificationStoreCapsPending(t *testing.T) {
	db := testutil.TestDB(t).Conn()
	store := notification.NewStore(db)
	ctx := context.Background()

	for i := 0; i < 205; i++ {
		if _, err := store.Enqueue(ctx, "webchat", "u1", "message"); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	n, err := store.CountPending(ctx, "webchat")
	if err != nil {
		t.Fatalf("CountPending: %v", err)
	}
	if n > 200 {
		t.Errorf("pending = %d, want <= 200", n)
	}
}
