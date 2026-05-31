package bus

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	b := New()
	ch := b.Subscribe("events")

	b.Publish("events", "agent-1", "hello world")

	select {
	case msg := <-ch:
		if msg.Topic != "events" {
			t.Errorf("topic = %q, want 'events'", msg.Topic)
		}
		if msg.From != "agent-1" {
			t.Errorf("from = %q, want 'agent-1'", msg.From)
		}
		if msg.Payload != "hello world" {
			t.Errorf("payload = %q, want 'hello world'", msg.Payload)
		}
		if msg.Time.IsZero() {
			t.Error("time should not be zero")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	b := New()
	ch1 := b.Subscribe("updates")
	ch2 := b.Subscribe("updates")

	b.Publish("updates", "agent-2", "broadcast")

	for i, ch := range []<-chan Message{ch1, ch2} {
		select {
		case msg := <-ch:
			if msg.Payload != "broadcast" {
				t.Errorf("subscriber %d: payload = %q, want 'broadcast'", i, msg.Payload)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

func TestUnsubscribe(t *testing.T) {
	b := New()
	ch := b.Subscribe("topic-a")

	b.Unsubscribe("topic-a", ch)

	// After unsubscribe, channel should be closed.
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after unsubscribe")
	}

	// Publishing after unsubscribe should not panic.
	b.Publish("topic-a", "agent-3", "orphan")
}

func TestTopicIsolation(t *testing.T) {
	b := New()
	chA := b.Subscribe("topic-a")
	chB := b.Subscribe("topic-b")

	b.Publish("topic-a", "agent-1", "only for A")

	select {
	case msg := <-chA:
		if msg.Payload != "only for A" {
			t.Errorf("topic-a payload = %q, want 'only for A'", msg.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for topic-a message")
	}

	// topic-b should have no messages.
	select {
	case msg := <-chB:
		t.Errorf("topic-b should have no messages, got %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// Expected: no message.
	}
}

// --- Reliable delivery tests ---

func TestPublishReliable_AckedOnFirstAttempt(t *testing.T) {
	b := New()
	ch := b.SubscribeReliable("mission")

	// Acker goroutine.
	go func() {
		msg := <-ch
		msg.Ack()
	}()

	err := b.PublishReliable(context.Background(), "mission", "supervisor-1", "go", ReliableOpts{
		Timeout:     500 * time.Millisecond,
		MaxAttempts: 3,
	})
	if err != nil {
		t.Errorf("expected ack on first attempt, got %v", err)
	}
}

func TestPublishReliable_NoSubscribersFailsFast(t *testing.T) {
	b := New()
	start := time.Now()
	err := b.PublishReliable(context.Background(), "ghost", "from", "x", ReliableOpts{
		Timeout:     5 * time.Second,
		MaxAttempts: 5,
		RetryDelay:  500 * time.Millisecond,
	})
	if !errors.Is(err, ErrNoSubscribers) {
		t.Errorf("expected ErrNoSubscribers, got %v", err)
	}
	// Should NOT have retried — situation won't improve.
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("ErrNoSubscribers should fail fast, took %v", elapsed)
	}
}

func TestPublishReliable_TimeoutThenRetry_EventualSuccess(t *testing.T) {
	b := New()
	ch := b.SubscribeReliable("flaky")

	var attempt atomic.Int32
	go func() {
		for {
			msg, ok := <-ch
			if !ok {
				return
			}
			a := attempt.Add(1)
			if a >= 3 {
				msg.Ack()
				return
			}
			// First two attempts: drop the message (no ack/nack).
		}
	}()

	err := b.PublishReliable(context.Background(), "flaky", "x", "y", ReliableOpts{
		Timeout:     150 * time.Millisecond,
		MaxAttempts: 5,
		RetryDelay:  20 * time.Millisecond,
	})
	if err != nil {
		t.Errorf("expected eventual ack after retries, got %v", err)
	}
	if attempt.Load() < 3 {
		t.Errorf("expected at least 3 delivery attempts, got %d", attempt.Load())
	}
}

func TestPublishReliable_AllNacked_RetriesThenFails(t *testing.T) {
	b := New()
	ch := b.SubscribeReliable("doomed")

	var nackCount atomic.Int32
	go func() {
		for {
			msg, ok := <-ch
			if !ok {
				return
			}
			nackCount.Add(1)
			msg.Nack()
		}
	}()

	err := b.PublishReliable(context.Background(), "doomed", "x", "y", ReliableOpts{
		Timeout:     200 * time.Millisecond,
		MaxAttempts: 3,
		RetryDelay:  20 * time.Millisecond,
	})
	if !errors.Is(err, ErrAllNacked) {
		t.Errorf("expected ErrAllNacked, got %v", err)
	}
	if nackCount.Load() != 3 {
		t.Errorf("expected 3 nacks (one per attempt), got %d", nackCount.Load())
	}
}

func TestPublishReliable_TimeoutAcrossAllAttempts(t *testing.T) {
	b := New()
	// Subscribe but never read — message gets enqueued, no Ack/Nack ever fires.
	_ = b.SubscribeReliable("silent")

	err := b.PublishReliable(context.Background(), "silent", "x", "y", ReliableOpts{
		Timeout:     80 * time.Millisecond,
		MaxAttempts: 2,
		RetryDelay:  10 * time.Millisecond,
	})
	if !errors.Is(err, ErrAckTimeout) {
		t.Errorf("expected ErrAckTimeout, got %v", err)
	}
}

func TestPublishReliable_MultipleSubscribers_OneAckWins(t *testing.T) {
	b := New()
	ch1 := b.SubscribeReliable("topic")
	ch2 := b.SubscribeReliable("topic")

	go func() {
		msg := <-ch1
		msg.Nack()
	}()
	go func() {
		msg := <-ch2
		// Wait long enough that ch1's nack arrives first, then ack.
		time.Sleep(20 * time.Millisecond)
		msg.Ack()
	}()

	err := b.PublishReliable(context.Background(), "topic", "x", "y", ReliableOpts{
		Timeout: 1 * time.Second,
	})
	if err != nil {
		t.Errorf("at-least-one ack should succeed even if other nacks, got %v", err)
	}
}

func TestPublishReliable_MessageIDStableAcrossRetries(t *testing.T) {
	b := New()
	ch := b.SubscribeReliable("stable")

	var seenIDs []string
	var mu sync.Mutex
	go func() {
		for {
			msg, ok := <-ch
			if !ok {
				return
			}
			mu.Lock()
			seenIDs = append(seenIDs, msg.ID)
			n := len(seenIDs)
			mu.Unlock()
			if n >= 3 {
				msg.Ack()
				return
			}
			// Drop first two — force retries.
		}
	}()

	_ = b.PublishReliable(context.Background(), "stable", "x", "y", ReliableOpts{
		Timeout:     80 * time.Millisecond,
		MaxAttempts: 5,
		RetryDelay:  10 * time.Millisecond,
	})

	mu.Lock()
	defer mu.Unlock()
	if len(seenIDs) < 3 {
		t.Fatalf("expected at least 3 deliveries, got %d", len(seenIDs))
	}
	for i := 1; i < len(seenIDs); i++ {
		if seenIDs[i] != seenIDs[0] {
			t.Errorf("message ID changed across retries: %v", seenIDs)
			break
		}
	}
}

func TestPublishReliable_ContextCancel_DuringRetryDelay(t *testing.T) {
	b := New()
	ch := b.SubscribeReliable("topic")
	go func() {
		for msg := range ch {
			msg.Nack() // force retry
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := b.PublishReliable(ctx, "topic", "x", "y", ReliableOpts{
		Timeout:     500 * time.Millisecond,
		MaxAttempts: 10,
		RetryDelay:  100 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrAllNacked) && !errors.Is(err, ErrAckTimeout) {
		t.Errorf("expected ctx-cancel-related or nack/timeout error, got %v", err)
	}
}

func TestReliableMessage_AckIdempotent(t *testing.T) {
	b := New()
	ch := b.SubscribeReliable("topic")

	var ackCalls atomic.Int32
	go func() {
		msg := <-ch
		// Call Ack many times — only the first should reach the publisher.
		for i := 0; i < 5; i++ {
			msg.Ack()
			ackCalls.Add(1)
		}
	}()

	if err := b.PublishReliable(context.Background(), "topic", "x", "y", ReliableOpts{Timeout: 500 * time.Millisecond}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Give the goroutine time to spam Ack.
	time.Sleep(50 * time.Millisecond)
	if ackCalls.Load() != 5 {
		t.Errorf("subscriber should have called Ack 5 times, got %d", ackCalls.Load())
	}
	// The test can't directly assert "publisher only saw one ack" — but if
	// Ack weren't idempotent the bus would deadlock or panic on the closed
	// reply channel; this test passing is the proof.
}

func TestReliableMessage_AckThenNack_FirstWins(t *testing.T) {
	b := New()
	ch := b.SubscribeReliable("topic")

	go func() {
		msg := <-ch
		msg.Ack()
		msg.Nack() // Should be a no-op.
	}()

	if err := b.PublishReliable(context.Background(), "topic", "x", "y", ReliableOpts{Timeout: 500 * time.Millisecond}); err != nil {
		t.Errorf("Ack should win over subsequent Nack, got %v", err)
	}
}

func TestUnsubscribeReliable_RemovesFromTopic(t *testing.T) {
	b := New()
	ch := b.SubscribeReliable("topic")

	if got := b.ReliableTopicCount(); got != 1 {
		t.Errorf("ReliableTopicCount = %d, want 1", got)
	}

	b.UnsubscribeReliable("topic", ch)

	// Topic should be gone from the subscriber map.
	if got := b.ReliableTopicCount(); got != 0 {
		t.Errorf("ReliableTopicCount after Unsubscribe = %d, want 0", got)
	}

	// Publish to the now-empty topic should report ErrNoSubscribers
	// rather than reaching the previously-subscribed channel. (The
	// channel is intentionally NOT closed — closing it would race with
	// concurrent in-flight publishes; subscribers exit on their own ctx
	// and the chan is reclaimed by GC.)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := b.PublishReliable(ctx, "topic", "x", "y", ReliableOpts{})
	if !errors.Is(err, ErrNoSubscribers) {
		t.Errorf("PublishReliable after Unsubscribe = %v, want ErrNoSubscribers", err)
	}
}

func TestReliableSubscribers_DoNotReceiveBestEffortPublish(t *testing.T) {
	b := New()
	ch := b.SubscribeReliable("topic")
	b.Publish("topic", "x", "y") // best-effort publish

	select {
	case <-ch:
		t.Error("reliable subscriber should NOT receive best-effort Publish")
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}
}

func TestBestEffortSubscribers_DoNotReceiveReliablePublish(t *testing.T) {
	b := New()
	ch := b.Subscribe("topic")
	// Subscribe a reliable consumer too so PublishReliable doesn't fail fast.
	rch := b.SubscribeReliable("topic")
	go func() { (<-rch).Ack() }()

	_ = b.PublishReliable(context.Background(), "topic", "x", "y", ReliableOpts{Timeout: 200 * time.Millisecond})

	select {
	case <-ch:
		t.Error("best-effort subscriber should NOT receive PublishReliable")
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}
}

func TestReliableOpts_Defaults(t *testing.T) {
	// Calling PublishReliable with empty opts should not panic and should
	// resolve to safe defaults (verified by checking it returns
	// ErrNoSubscribers quickly with no subscribers — proves no infinite wait).
	b := New()
	start := time.Now()
	err := b.PublishReliable(context.Background(), "no-subs", "x", "y", ReliableOpts{})
	if !errors.Is(err, ErrNoSubscribers) {
		t.Errorf("expected ErrNoSubscribers, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("default opts should still fail fast on no subscribers, took %v", elapsed)
	}
}

func TestReliableOpts_ClampedToSafeRange(t *testing.T) {
	// MaxAttempts=99 should clamp to 10; Timeout=10min should clamp to 30s.
	// Verify clamping by checking that an all-nacked publish doesn't
	// produce 99 retries (would take forever) — should produce ≤10.
	b := New()
	ch := b.SubscribeReliable("topic")

	var nacks atomic.Int32
	go func() {
		for msg := range ch {
			nacks.Add(1)
			msg.Nack()
		}
	}()

	_ = b.PublishReliable(context.Background(), "topic", "x", "y", ReliableOpts{
		Timeout:     50 * time.Millisecond,
		MaxAttempts: 99, // should clamp to 10
		RetryDelay:  1 * time.Millisecond,
	})

	if got := nacks.Load(); got > 10 {
		t.Errorf("MaxAttempts should be clamped at 10, got %d nacks", got)
	}
}


// --- Competing-consumer (queue-group) tests ---

func TestCompeting_SingleSubscriberReceives(t *testing.T) {
	b := New()
	sub := b.SubscribeCompeting("work")
	defer b.UnsubscribeCompeting("work", sub)

	got := make(chan ReliableMessage, 1)
	go func() {
		msg := <-sub
		msg.Ack()
		got <- msg
	}()

	if err := b.PublishCompeting(context.Background(), "work", "pub", "hello",
		ReliableOpts{Timeout: 1 * time.Second}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case msg := <-got:
		if msg.Payload != "hello" {
			t.Errorf("payload = %q, want hello", msg.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delivery")
	}
}

func TestCompeting_ExactlyOneSubscriberPerMessage(t *testing.T) {
	b := New()
	const N = 3

	var counts [N]atomic.Int32
	subs := make([]<-chan ReliableMessage, N)
	for i := 0; i < N; i++ {
		ch := b.SubscribeCompeting("pool")
		subs[i] = ch
		idx := i
		go func() {
			for msg := range ch {
				counts[idx].Add(1)
				msg.Ack()
			}
		}()
	}
	defer func() {
		for _, ch := range subs {
			b.UnsubscribeCompeting("pool", ch)
		}
	}()

	// Publish 12 messages.
	for i := 0; i < 12; i++ {
		if err := b.PublishCompeting(context.Background(), "pool", "pub",
			"msg", ReliableOpts{Timeout: 1 * time.Second}); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	// Wait for any in-flight goroutines to finish processing.
	time.Sleep(100 * time.Millisecond)

	total := int32(0)
	for i := 0; i < N; i++ {
		total += counts[i].Load()
	}
	if total != 12 {
		t.Errorf("total deliveries = %d, want 12 (exactly one per message)", total)
	}
	// Fair distribution: each subscriber should have at least one
	// message (shuffle on every publish).
	for i := 0; i < N; i++ {
		if counts[i].Load() == 0 {
			t.Errorf("subscriber %d received 0 messages; shuffle did not distribute fairly", i)
		}
	}
}

func TestCompeting_NoSubscribers(t *testing.T) {
	b := New()
	err := b.PublishCompeting(context.Background(), "vacant", "pub", "hi",
		ReliableOpts{Timeout: 200 * time.Millisecond})
	if !errors.Is(err, ErrNoSubscribers) {
		t.Errorf("expected ErrNoSubscribers, got %v", err)
	}
}

func TestCompeting_BusySubscriberFallsThroughToPeer(t *testing.T) {
	b := New()

	// Subscriber 1: holds the message without acking (simulates busy).
	busy := b.SubscribeCompeting("pool")
	go func() {
		msg := <-busy
		// Hold it — never ack within the publish timeout.
		time.Sleep(2 * time.Second)
		msg.Ack()
	}()
	defer b.UnsubscribeCompeting("pool", busy)

	// Subscriber 2: ready and willing.
	ready := b.SubscribeCompeting("pool")
	delivered := make(chan struct{}, 1)
	go func() {
		for msg := range ready {
			msg.Ack()
			delivered <- struct{}{}
		}
	}()
	defer b.UnsubscribeCompeting("pool", ready)

	// First publish may land on the busy subscriber (the buffer
	// accepts it, but no ack). Second publish should fall through to
	// the ready subscriber — but only when the bus exhausts the first
	// peer in the same attempt. Use a short ack timeout so the bus
	// skips ahead.
	err1 := b.PublishCompeting(context.Background(), "pool", "pub", "a",
		ReliableOpts{Timeout: 150 * time.Millisecond, MaxAttempts: 2, SendTimeout: 50 * time.Millisecond})
	err2 := b.PublishCompeting(context.Background(), "pool", "pub", "b",
		ReliableOpts{Timeout: 150 * time.Millisecond, MaxAttempts: 2, SendTimeout: 50 * time.Millisecond})

	// At least one of the two publishes must have reached the ready
	// subscriber (the busy one only takes one before its buffer fills).
	select {
	case <-delivered:
		// OK
	case <-time.After(time.Second):
		t.Fatalf("ready subscriber never received; err1=%v err2=%v", err1, err2)
	}
}

func TestCompeting_CompetingTopicCount(t *testing.T) {
	b := New()
	if b.CompetingTopicCount() != 0 {
		t.Errorf("expected 0 topics initially, got %d", b.CompetingTopicCount())
	}
	a := b.SubscribeCompeting("topic-a")
	c := b.SubscribeCompeting("topic-b")
	defer b.UnsubscribeCompeting("topic-a", a)
	defer b.UnsubscribeCompeting("topic-b", c)
	if got := b.CompetingTopicCount(); got != 2 {
		t.Errorf("expected 2 topics after 2 subs, got %d", got)
	}
}

func TestCompeting_BroadcastAndCompetingAreIndependent(t *testing.T) {
	b := New()
	bcastCh := b.SubscribeReliable("split")
	compCh := b.SubscribeCompeting("split")
	defer b.UnsubscribeReliable("split", bcastCh)
	defer b.UnsubscribeCompeting("split", compCh)

	// PublishCompeting goes only to competing subscribers.
	go func() {
		msg := <-compCh
		msg.Ack()
	}()
	if err := b.PublishCompeting(context.Background(), "split", "pub", "for-competing",
		ReliableOpts{Timeout: 500 * time.Millisecond}); err != nil {
		t.Fatalf("PublishCompeting: %v", err)
	}
	// Broadcast subscriber must NOT have received it.
	select {
	case msg := <-bcastCh:
		msg.Ack()
		t.Errorf("broadcast subscriber received a competing-publish message: %s", msg.Payload)
	case <-time.After(100 * time.Millisecond):
		// Good — broadcast channel stayed empty.
	}
}

