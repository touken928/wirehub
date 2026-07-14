package service

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/touken928/wirehub/internal/config"
	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
	"github.com/touken928/wirehub/internal/repo"
)

func testKeyGenerator() (string, string, error) {
	return "private", "public", nil
}

func newUnconfiguredApp(t *testing.T, generator KeyGenerator) *App {
	t.Helper()
	store, err := repo.New(&config.RuntimeConfig{DatabasePath: filepath.Join(t.TempDir(), "wirehub.db")})
	if err != nil {
		t.Fatal(err)
	}
	if generator == nil {
		generator = testKeyGenerator
	}
	return NewApp(store, generator)
}

type keyGenerationRuntime struct {
	mu       sync.Mutex
	actions  map[string]int
	startErr error
	stopErr  error
}

func newKeyGenerationRuntime() *keyGenerationRuntime {
	return &keyGenerationRuntime{actions: make(map[string]int)}
}

func (r *keyGenerationRuntime) record(action string) {
	r.mu.Lock()
	r.actions[action]++
	r.mu.Unlock()
}

func (r *keyGenerationRuntime) Start(domainruntime.SyncBundle) error {
	r.record("start")
	return r.startErr
}
func (r *keyGenerationRuntime) Stop() error {
	r.record("stop")
	return r.stopErr
}
func (r *keyGenerationRuntime) SetDNSUpstream([]string) {
	r.record("dns-upstream")
}

func (r *keyGenerationRuntime) actionCount(action string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.actions[action]
}

func assertNoRuntimeActions(t *testing.T, runtime *keyGenerationRuntime) {
	t.Helper()
	for _, action := range []string{"start", "stop", "dns-upstream"} {
		if count := runtime.actionCount(action); count != 0 {
			t.Errorf("runtime %s calls = %d, want 0", action, count)
		}
	}
}

func TestSetupKeyGenerationFailureHasNoSideEffects(t *testing.T) {
	keyErr := errors.New("key generation failed")
	app := newUnconfiguredApp(t, func() (string, string, error) {
		return "", "", keyErr
	})
	net := newKeyGenerationRuntime()
	app.Hub.SetNetworkRuntime(net)
	dp := &characterizationDataplane{}
	app.Hub.dpMu.Lock()
	app.Hub.liveDP = dp
	app.Hub.dpMu.Unlock()

	if err := app.Setup(transitionSetupInput()); !errors.Is(err, keyErr) {
		t.Fatalf("Setup error = %v, want %v", err, keyErr)
	}
	configured, err := app.IsConfigured()
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		t.Fatal("setup persisted configuration after key-generation failure")
	}
	assertNoRuntimeActions(t, net)
	if len(dp.fullSyncs) != 0 || dp.applyPolicies != 0 || dp.dnsUpdates != 0 {
		t.Fatalf("dataplane actions full_sync=%d apply_policy=%d update_dns=%d", len(dp.fullSyncs), dp.applyPolicies, dp.dnsUpdates)
	}
}

func TestCreatePeerKeyGenerationFailureHasNoSideEffects(t *testing.T) {
	keyErr := errors.New("key generation failed")
	app := testApp(t)
	app.keyGenerator = func() (string, string, error) {
		return "", "", keyErr
	}
	group, err := app.store.CreateGroup("users", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	net := newKeyGenerationRuntime()
	app.Hub.SetNetworkRuntime(net)
	dp := &characterizationDataplane{}
	app.Hub.dpMu.Lock()
	app.Hub.liveDP = dp
	app.Hub.dpMu.Unlock()

	if _, err := app.CreatePeer(CreatePeerInput{Name: "alpha", GroupID: group.ID}); !errors.Is(err, keyErr) {
		t.Fatalf("CreatePeer error = %v, want %v", err, keyErr)
	}
	peers, err := app.store.ListPeers()
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 0 {
		t.Fatalf("persisted peers = %d, want 0", len(peers))
	}
	assertNoRuntimeActions(t, net)
	if len(dp.fullSyncs) != 0 || dp.applyPolicies != 0 || dp.dnsUpdates != 0 {
		t.Fatalf("dataplane actions full_sync=%d apply_policy=%d update_dns=%d", len(dp.fullSyncs), dp.applyPolicies, dp.dnsUpdates)
	}
}

func TestSetupInjectedKeysPersist(t *testing.T) {
	app := newUnconfiguredApp(t, func() (string, string, error) {
		return "setup-private", "setup-public", nil
	})
	app.Hub.SetNetworkRuntime(newKeyGenerationRuntime())
	if err := app.Setup(transitionSetupInput()); err != nil {
		t.Fatal(err)
	}
	settings, err := app.store.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ServerPrivateKey != "setup-private" || settings.ServerPublicKey != "setup-public" {
		t.Fatalf("persisted keys = %q/%q", settings.ServerPrivateKey, settings.ServerPublicKey)
	}
}

func TestCreatePeerInjectedKeysPersist(t *testing.T) {
	app := testApp(t)
	app.keyGenerator = func() (string, string, error) {
		return "peer-private", "peer-public", nil
	}
	group, err := app.store.CreateGroup("users", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := app.CreatePeer(CreatePeerInput{Name: "alpha", GroupID: group.ID})
	if err != nil {
		t.Fatal(err)
	}
	if peer.PublicKey != "peer-public" || peer.FQDN != "alpha.wirehub" {
		t.Fatalf("created view = %+v", peer)
	}
	persisted, err := app.store.GetPeer(peer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.PrivateKey != "peer-private" || persisted.PublicKey != "peer-public" {
		t.Fatalf("persisted keys = %q/%q", persisted.PrivateKey, persisted.PublicKey)
	}
}
