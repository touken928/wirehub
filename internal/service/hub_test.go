package service

import (
	"sync"
	"testing"
	"time"

	dompolicy "github.com/touken928/wirehub/internal/domain/policy"
	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
	"github.com/touken928/wirehub/internal/vpn/tunnel"
)

type blockedStatsDataplane struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (d *blockedStatsDataplane) Start(domainruntime.SyncBundle) error         { return nil }
func (d *blockedStatsDataplane) Stop() error                                  { return nil }
func (d *blockedStatsDataplane) ReloadSettings() error                        { return nil }
func (d *blockedStatsDataplane) SyncPortForwards() error                      { return nil }
func (d *blockedStatsDataplane) SyncMaps() error                              { return nil }
func (d *blockedStatsDataplane) HubListenPort() int                           { return 0 }
func (d *blockedStatsDataplane) SyncPeer(domainruntime.WGPeer) error          { return nil }
func (d *blockedStatsDataplane) RemovePeer(string) error                      { return nil }
func (d *blockedStatsDataplane) ApplyPolicy(dompolicy.AccessPolicySpec) error { return nil }
func (d *blockedStatsDataplane) UpdateDNS(domainruntime.DNSCatalog, []domainruntime.WGPeer) error {
	return nil
}
func (d *blockedStatsDataplane) FullSync(domainruntime.SyncBundle) error { return nil }
func (d *blockedStatsDataplane) GetStats() (map[string]tunnel.PeerStats, error) {
	d.once.Do(func() { close(d.started) })
	<-d.release
	return nil, nil
}

// countingPublisher implements StatusPublisher with a call counter.
type countingPublisher struct {
	mu    sync.Mutex
	count int
}

func (c *countingPublisher) Publish() {
	c.mu.Lock()
	c.count++
	c.mu.Unlock()
}

func (c *countingPublisher) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

func TestStopStatusPoller_Idempotent(t *testing.T) {
	h := &Hub{}
	// Multiple stops must not panic
	h.StopStatusPoller()
	h.StopStatusPoller()
	// Start then stop
	h.StartStatusPoller(3600) // long interval so it never fires during test
	h.StopStatusPoller()
	h.StopStatusPoller() // second stop must be safe
}

func TestStartStatusPoller_Idempotent(t *testing.T) {
	h := &Hub{}
	// Double start must not panic or leave dangling goroutines
	h.StartStatusPoller(3600)
	h.StartStatusPoller(3600) // no-op, must not panic
	h.StopStatusPoller()
	// One stop is sufficient regardless of how many starts
}

func TestStartStopStatusPoller_Restart(t *testing.T) {
	h := &Hub{}
	// Start with short interval, stop, restart — must not panic or deadlock
	h.StartStatusPoller(1)
	time.Sleep(200 * time.Millisecond)
	h.StopStatusPoller()

	// Restart
	h.StartStatusPoller(1)
	time.Sleep(200 * time.Millisecond)
	h.StopStatusPoller()
}

func TestStopStatusPollerWaitsForBlockedStats(t *testing.T) {
	h := &Hub{}
	dp := &blockedStatsDataplane{started: make(chan struct{}), release: make(chan struct{})}
	h.dpMu.Lock()
	h.liveDP = dp
	h.dpMu.Unlock()
	h.StartStatusPoller(1)
	select {
	case <-dp.started:
	case <-time.After(2 * time.Second):
		t.Fatal("poller did not enter blocked stats call")
	}
	stopDone := make(chan struct{})
	go func() {
		h.StopStatusPoller()
		close(stopDone)
	}()
	select {
	case <-stopDone:
		t.Fatal("stopping poller returned before blocked stats completed")
	case <-time.After(500 * time.Millisecond):
	}
	close(dp.release)
	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("stopping poller did not complete after stats returned")
	}
	// Start must wait for the old poller to exit before creating a new one.
	h.StartStatusPoller(3600)
	h.StopStatusPoller()
}

func TestStartStopStatusPoller_ConcurrentStop(t *testing.T) {
	h := &Hub{}
	var wg sync.WaitGroup

	// Start poller once
	h.StartStatusPoller(1)

	// Concurrent stop calls — must not panic
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.StopStatusPoller()
		}()
	}
	wg.Wait()

	// Must be able to restart after concurrent stop
	h.StartStatusPoller(1)
	h.StopStatusPoller()
}

func TestStartStatusPoller_ConcurrentStart(t *testing.T) {
	h := &Hub{}
	var wg sync.WaitGroup

	// Concurrent start calls must not create multiple pollers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.StartStatusPoller(1)
		}()
	}
	wg.Wait()

	// Stop once — if idempotent, one stop is enough
	h.StopStatusPoller()
}

func TestStartStopStatusPoller_Race(t *testing.T) {
	h := &Hub{}
	var wg sync.WaitGroup

	// Start and stop racing against each other
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.StartStatusPoller(1)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.StopStatusPoller()
		}()
	}
	wg.Wait()
	// Must not deadlock or panic
	h.StopStatusPoller()
}

func TestHubDataplane_NilSafe(t *testing.T) {
	h := &Hub{}
	dp := h.dataplane()
	if dp != nil {
		t.Fatal("expected nil dataplane on fresh Hub")
	}
	// onStopped with no prior start must be safe
	h.onStopped()
	if nc := h.NetworkRuntime(); nc != nil {
		t.Fatal("expected nil NetworkRuntime on fresh Hub")
	}
}

func TestHubStatusPoller_UnsafeInterval(t *testing.T) {
	h := &Hub{}
	// Zero interval should not panic (previously caused NewTicker panic)
	h.StartStatusPoller(0)
	h.StopStatusPoller()
	// Negative should not panic
	h.StartStatusPoller(-1)
	h.StopStatusPoller()
}

func TestHubNetworkRuntime_NilSafe(t *testing.T) {
	h := &Hub{}
	// SyncPortForwards on nil network should not panic or return error
	if err := h.SyncPortForwards(); err != nil {
		t.Fatalf("SyncPortForwards on nil network should return nil, got %v", err)
	}
	// SetDNSUpstream on nil network must not panic
	h.SetDNSUpstream(nil)
	// NetworkRuntime on fresh hub must be nil
	if nc := h.NetworkRuntime(); nc != nil {
		t.Fatal("expected nil NetworkRuntime on fresh Hub")
	}
}

// Test that the inner poller goroutine does not panic with a nil dataplane.
func TestHubPollPeerStats_NilDataplane(t *testing.T) {
	h := &Hub{}
	// pollPeerStats should handle nil dataplane without panic
	h.pollPeerStats()
}

// race-free baseline for go test -race.
func TestStatusPoller_NoRace(t *testing.T) {
	h := &Hub{}
	h.SetStatusPublisher(&countingPublisher{})
	h.StartStatusPoller(3600)
	// Read dataplane concurrently
	go func() {
		_ = h.dataplane()
	}()
	// Read network concurrently
	go func() {
		_ = h.NetworkRuntime()
	}()
	time.Sleep(10 * time.Millisecond)
	h.StopStatusPoller()
}
