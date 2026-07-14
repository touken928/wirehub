package service

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	dompolicy "github.com/touken928/wirehub/internal/domain/policy"
	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
)

type phase5Network struct {
	mu        sync.Mutex
	startErr  error
	stopErr   error
	stopCount int
	starts    []domainruntime.SyncBundle
	onStart   func(domainruntime.SyncBundle)
}

func (n *phase5Network) Start(bundle domainruntime.SyncBundle) error {
	n.mu.Lock()
	n.starts = append(n.starts, bundle)
	fn, err := n.onStart, n.startErr
	n.mu.Unlock()
	if err == nil && fn != nil {
		fn(bundle)
	}
	return err
}
func (n *phase5Network) Stop() error {
	n.mu.Lock()
	n.stopCount++
	err := n.stopErr
	n.mu.Unlock()
	return err
}
func (n *phase5Network) SetDNSUpstream([]string) {}

type phase5Dataplane struct {
	fullSyncErr error
}

func (d *phase5Dataplane) Start(domainruntime.SyncBundle) error         { return nil }
func (d *phase5Dataplane) Stop() error                                  { return nil }
func (d *phase5Dataplane) ApplyPolicy(dompolicy.AccessPolicySpec) error { return nil }
func (d *phase5Dataplane) UpdateDNS(domainruntime.DNSCatalog, []domainruntime.WGPeer) error {
	return nil
}
func (d *phase5Dataplane) FullSync(domainruntime.SyncBundle) error               { return d.fullSyncErr }
func (d *phase5Dataplane) GetStats() (map[string]domainruntime.PeerStats, error) { return nil, nil }

func installPhase5Dataplane(a *App, fullSyncErr error) {
	a.Hub.dpMu.Lock()
	a.Hub.liveDP = &phase5Dataplane{fullSyncErr: fullSyncErr}
	a.Hub.dpMu.Unlock()
}

func TestPhase5SyncFailureRestartsWithSameDesiredBundle(t *testing.T) {
	a := testApp(t)
	g, err := a.store.CreateGroup("users", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	net := &phase5Network{}
	net.onStart = func(domainruntime.SyncBundle) { installPhase5Dataplane(a, nil) }
	a.Hub.SetNetworkRuntime(net)
	installPhase5Dataplane(a, errors.New("peer sync failed"))

	_, err = a.CreatePeer(CreatePeerInput{Name: "alpha", GroupID: g.ID})
	if err != nil {
		t.Fatalf("restart should converge: %v", err)
	}
	bundle, loadErr := a.LoadSyncBundle()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	net.mu.Lock()
	starts := append([]domainruntime.SyncBundle(nil), net.starts...)
	stopCount := net.stopCount
	net.mu.Unlock()
	if stopCount != 1 || len(starts) != 1 || !reflect.DeepEqual(starts[0], bundle) {
		t.Fatalf("restart stop=%d starts=%d bundleMatch=%v", stopCount, len(starts), len(starts) == 1 && reflect.DeepEqual(starts[0], bundle))
	}
	peers, err := a.store.ListPeers()
	if err != nil || len(peers) != 1 {
		t.Fatalf("persisted peers = %d, err=%v; requested state was lost", len(peers), err)
	}
}

func TestPhase5MapAndForwardSyncFailuresPersistRequestedState(t *testing.T) {
	a := testApp(t)
	g, err := a.store.CreateGroup("users", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	net := &phase5Network{}
	net.onStart = func(domainruntime.SyncBundle) { installPhase5Dataplane(a, nil) }
	a.Hub.SetNetworkRuntime(net)
	installPhase5Dataplane(a, errors.New("map sync failed"))

	_, err = a.CreateServiceMap(MapInput{Slug: "svc", TargetHost: "10.0.0.2", AllowedGroupIDs: []uint{g.ID}})
	if err != nil {
		t.Fatalf("map restart should converge: %v", err)
	}
	maps, err := a.store.ListServiceMaps()
	if err != nil || len(maps) != 1 {
		t.Fatalf("persisted maps = %d, err=%v", len(maps), err)
	}

	installPhase5Dataplane(a, errors.New("forward sync failed"))
	_, err = a.CreatePortForward(PortForwardInput{ListenPort: 19090, Protocol: "tcp", TargetHost: "10.0.0.2", TargetPort: 80})
	if err != nil {
		t.Fatalf("forward restart should converge: %v", err)
	}
	forwards, err := a.store.ListPortForwards()
	if err != nil || len(forwards) != 1 {
		t.Fatalf("persisted forwards = %d, err=%v", len(forwards), err)
	}
}

func TestPhase5SettingsRestartFailureKeepsRequestedSettings(t *testing.T) {
	a := testSettingsApp(t)
	net := &phase5Network{startErr: errors.New("restart failed")}
	a.Hub.SetNetworkRuntime(net)

	_, err := a.UpdateMutableSettings(1400, 2, []string{"1.1.1.1"})
	if !IsRuntimeMutationError(err) {
		t.Fatalf("settings error = %v, want RuntimeMutationError", err)
	}
	if net.stopCount != 2 {
		t.Fatalf("runtime stop count = %d, want 2", net.stopCount)
	}
	settings, err := a.store.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.MTU != 1400 || settings.StatusInterval != 2 {
		t.Fatalf("settings = %+v, want requested persisted values", settings)
	}
}

func TestPhase5StopFailureLeavesRuntimeStateUnknown(t *testing.T) {
	a := testApp(t)
	g, err := a.store.CreateGroup("users", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	net := &phase5Network{stopErr: errors.New("stop failed")}
	a.Hub.SetNetworkRuntime(net)
	installPhase5Dataplane(a, errors.New("sync failed"))
	_, err = a.CreatePeer(CreatePeerInput{Name: "unknown", GroupID: g.ID})
	var recovery *RuntimeRecoveryError
	if !errors.As(err, &recovery) {
		t.Fatalf("error = %v, want RuntimeRecoveryError", err)
	}
	if IsRuntimeMutationError(err) {
		t.Fatal("stop failure must not claim runtime stopped")
	}
	a.Hub.dpMu.RLock()
	live := a.Hub.liveDP
	a.Hub.dpMu.RUnlock()
	if live == nil {
		t.Fatal("live dataplane was cleared despite unknown runtime state")
	}
}
