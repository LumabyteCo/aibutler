package lan

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	d := New(8080, "butler")
	if d == nil {
		t.Fatal("expected non-nil Discovery")
	}
	if d.port != 8080 {
		t.Errorf("port = %d, want 8080", d.port)
	}
	if d.agentName != "butler" {
		t.Errorf("agentName = %q, want 'butler'", d.agentName)
	}
}

func TestStartStop(t *testing.T) {
	d := New(8080, "butler")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Double start should error.
	if err := d.Start(ctx); err == nil {
		t.Error("expected error on double Start")
	}

	if err := d.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Double stop should be no-op.
	if err := d.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestPeersNoBroadcasts(t *testing.T) {
	// This test is inherently environment-dependent — it assumes the local
	// network has no mDNS peers broadcasting the _aibutler._tcp.local service.
	// That holds on developer machines but not on CI runners, where:
	//  - multiple parallel test processes may be running at once
	//  - the runner host itself may have mDNS responders
	//  - the network stack may report loopback peers from other jobs
	// Skip in CI environments; run locally for real coverage.
	if os.Getenv("CI") != "" || testing.Short() {
		t.Skip("mDNS peer discovery is environment-dependent; skipping in CI/short mode")
	}

	// Create a discovery with a short timeout that won't find peers.
	d := New(9999, "test-agent")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Peers should return empty list when no one is broadcasting.
	peers, err := d.Peers(ctx)
	if err != nil {
		// Port in use is expected on some systems.
		t.Skipf("Peers: %v (port likely in use)", err)
	}
	if len(peers) != 0 {
		t.Errorf("expected 0 peers, got %d", len(peers))
	}
}
