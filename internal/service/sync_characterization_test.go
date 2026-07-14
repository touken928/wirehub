package service

import (
	"errors"
	"net/netip"
	"reflect"
	"testing"

	"github.com/touken928/wirehub/internal/config"
	dompolicy "github.com/touken928/wirehub/internal/domain/policy"
	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
	"github.com/touken928/wirehub/internal/repo"
)

func TestLoadSyncBundleCharacterization(t *testing.T) {
	app := testApp(t)
	settings, err := app.store.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.MTU = 0
	settings.StatusInterval = 0
	settings.UpstreamDNS = []string{"1.1.1.1", "8.8.8.8"}
	if err := app.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}

	groupOne, err := app.store.CreateGroup("source", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	groupTwo, err := app.store.CreateGroup("target", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.UpsertGroupLink(groupOne.ID, groupTwo.ID, false); err != nil {
		t.Fatal(err)
	}
	peerOne := &repo.Peer{Name: "alpha", PublicKey: "pub-alpha", PrivateKey: "priv-alpha", WGIP: "100.127.0.2", GroupID: groupOne.ID, Enabled: true, DNSName: "alpha"}
	peerTwo := &repo.Peer{Name: "beta", PublicKey: "pub-beta", PrivateKey: "priv-beta", WGIP: "100.127.0.3", GroupID: groupTwo.ID, Enabled: true, DNSName: "beta"}
	if err := app.store.CreatePeer(peerOne); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreatePeer(peerTwo); err != nil {
		t.Fatal(err)
	}
	mapDetail, err := app.store.CreateServiceMap(repo.MapInput{Slug: "portal", TargetHost: "10.0.0.10", AllowedGroups: []uint{groupOne.ID}})
	if err != nil {
		t.Fatal(err)
	}
	forward, err := app.store.CreatePortForward(51820, repo.PortForwardInput{ListenPort: 19090, Protocol: "tcp", TargetHost: "10.0.0.10", TargetPort: 8080})
	if err != nil {
		t.Fatal(err)
	}

	bundle, err := app.LoadSyncBundle()
	if err != nil {
		t.Fatal(err)
	}
	wantSettings := domainruntime.NetworkSettings{
		HubIP:            settings.HubIP,
		DNSIP:            settings.DNSIP,
		WGSubnet:         settings.WGSubnet,
		ServerPrivateKey: settings.ServerPrivateKey,
		MTU:              config.DefaultMTU,
		ListenPort:       settings.ListenPort,
		StatusInterval:   config.DefaultStatusInterval,
		UpstreamDNS:      []string{"1.1.1.1", "8.8.8.8"},
	}
	if !reflect.DeepEqual(bundle.Settings, wantSettings) {
		t.Fatalf("settings = %+v, want %+v", bundle.Settings, wantSettings)
	}
	wantPeers := map[uint]domainruntime.WGPeer{
		peerOne.ID: {ID: peerOne.ID, PublicKey: "pub-alpha", WGIP: "100.127.0.2", DNSName: "alpha", GroupID: groupOne.ID, Enabled: true},
		peerTwo.ID: {ID: peerTwo.ID, PublicKey: "pub-beta", WGIP: "100.127.0.3", DNSName: "beta", GroupID: groupTwo.ID, Enabled: true},
	}
	gotPeers := make(map[uint]domainruntime.WGPeer, len(bundle.Peers))
	for _, peer := range bundle.Peers {
		gotPeers[peer.ID] = peer
	}
	if len(gotPeers) != len(bundle.Peers) || !reflect.DeepEqual(gotPeers, wantPeers) {
		t.Fatalf("peers = %+v", bundle.Peers)
	}
	wantTransparentPeers := map[string]uint{"100.127.0.2": groupOne.ID, "100.127.0.3": groupTwo.ID}
	if len(bundle.Policy.Transparent.Peers) != len(wantTransparentPeers) || len(bundle.Policy.Transparent.UniLinks) != 1 {
		t.Fatalf("ACL transparent = %+v", bundle.Policy.Transparent)
	}
	seenTransparentPeers := make(map[string]struct{}, len(bundle.Policy.Transparent.Peers))
	for _, peer := range bundle.Policy.Transparent.Peers {
		address := peer.WGIP.String()
		wantGroup, knownAddress := wantTransparentPeers[address]
		if !knownAddress {
			t.Fatalf("unexpected transparent peer = %+v", peer)
		}
		if _, duplicate := seenTransparentPeers[address]; duplicate || peer.GroupID != wantGroup {
			t.Fatalf("unexpected transparent peer = %+v", peer)
		}
		seenTransparentPeers[address] = struct{}{}
	}
	if !reflect.DeepEqual(bundle.Policy.Transparent.UniLinks, []dompolicy.GroupLinkPair{{FromGroupID: groupOne.ID, ToGroupID: groupTwo.ID, Bidirectional: false}}) {
		t.Fatalf("transparent links = %+v", bundle.Policy.Transparent.UniLinks)
	}
	wantBlocked := map[netip.Addr]map[netip.Addr]struct{}{
		parseRuntimeAddr(t, "100.127.0.3"): {
			parseRuntimeAddr(t, "100.127.0.2"):       {},
			parseRuntimeAddr(t, mapDetail.VirtualIP): {},
		},
	}
	gotBlocked := make(map[netip.Addr]map[netip.Addr]struct{}, len(bundle.Policy.Blocked))
	for from, destinations := range bundle.Policy.Blocked {
		gotBlocked[from] = make(map[netip.Addr]struct{}, len(destinations))
		for _, destination := range destinations {
			gotBlocked[from][destination] = struct{}{}
		}
	}
	if !reflect.DeepEqual(gotBlocked, wantBlocked) {
		t.Fatalf("blocked ACL = %v, want %v", gotBlocked, wantBlocked)
	}
	if !reflect.DeepEqual(bundle.Forwards, []domainruntime.ForwardRule{{ID: forward.ID, ListenPort: 19090, Protocol: "tcp", TargetHost: "10.0.0.10", TargetPort: 8080}}) {
		t.Fatalf("forwards = %+v", bundle.Forwards)
	}
	wantMaps := []domainruntime.MapRule{{ID: mapDetail.ID, Slug: "portal", TargetHost: "10.0.0.10", VirtualIP: mapDetail.VirtualIP, AllowedGroupIDs: map[uint]struct{}{groupOne.ID: {}}}}
	if len(bundle.Maps) != len(wantMaps) || !reflect.DeepEqual(bundle.Maps, wantMaps) {
		t.Fatalf("maps = %+v", bundle.Maps)
	}
	wantDNS := domainruntime.DNSCatalog{
		HubIP: settings.HubIP,
		Peers: map[string]string{"alpha": "100.127.0.2", "beta": "100.127.0.3"},
		Maps:  map[string]domainruntime.MapDNSEntry{"portal": {VirtualIP: mapDetail.VirtualIP, AllowedGroupIDs: map[uint]struct{}{groupOne.ID: {}}}},
	}
	if !reflect.DeepEqual(bundle.DNS, wantDNS) {
		t.Fatalf("DNS = %+v, want %+v", bundle.DNS, wantDNS)
	}
}

func parseRuntimeAddr(t *testing.T, value string) (addr netip.Addr) {
	t.Helper()
	addr, err := netip.ParseAddr(value)
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

type characterizationDataplane struct {
	fullSyncErr   error
	fullSyncs     []domainruntime.SyncBundle
	applyPolicies int
	dnsUpdates    int
}

func (d *characterizationDataplane) Start(domainruntime.SyncBundle) error { return nil }
func (d *characterizationDataplane) Stop() error                          { return nil }
func (d *characterizationDataplane) ApplyPolicy(dompolicy.AccessPolicySpec) error {
	d.applyPolicies++
	return nil
}
func (d *characterizationDataplane) UpdateDNS(domainruntime.DNSCatalog, []domainruntime.WGPeer) error {
	d.dnsUpdates++
	return nil
}
func (d *characterizationDataplane) FullSync(bundle domainruntime.SyncBundle) error {
	d.fullSyncs = append(d.fullSyncs, bundle)
	return d.fullSyncErr
}
func (d *characterizationDataplane) GetStats() (map[string]domainruntime.PeerStats, error) {
	return nil, nil
}

func TestReconcileRuntimeCharacterization(t *testing.T) {
	fullSyncErr := errors.New("full sync failed")
	startErr := errors.New("restart failed")
	stopErr := errors.New("stop failed")
	tests := []struct {
		name          string
		configured    bool
		fullSyncErr   error
		startErr      error
		stopErr       error
		wantErr       func(error) bool
		wantStarts    int
		wantStops     int
		wantFullSyncs int
	}{
		{name: "full sync success", configured: true, wantStarts: 0, wantStops: 0, wantFullSyncs: 1},
		{name: "fallback restart", configured: true, fullSyncErr: fullSyncErr, wantStarts: 1, wantStops: 1, wantFullSyncs: 1},
		{name: "failed restart", configured: true, fullSyncErr: fullSyncErr, startErr: startErr, wantStarts: 1, wantStops: 2, wantFullSyncs: 1, wantErr: func(err error) bool { var target *RuntimeMutationError; return errors.As(err, &target) }},
		{name: "failed stop", configured: true, fullSyncErr: fullSyncErr, stopErr: stopErr, wantStarts: 0, wantStops: 1, wantFullSyncs: 1, wantErr: func(err error) bool { var target *RuntimeRecoveryError; return errors.As(err, &target) }},
		{name: "bundle load failure", configured: false, wantStarts: 0, wantStops: 1, wantFullSyncs: 0, wantErr: func(err error) bool { var target *RuntimeStoppedError; return errors.As(err, &target) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var app *App
			if test.configured {
				app = testApp(t)
			} else {
				app = newUnconfiguredApp(t, nil)
			}
			net := &phase5Network{startErr: test.startErr, stopErr: test.stopErr}
			dp := &characterizationDataplane{fullSyncErr: test.fullSyncErr}
			app.Hub.SetNetworkRuntime(net)
			app.Hub.dpMu.Lock()
			app.Hub.liveDP = dp
			app.Hub.dpMu.Unlock()

			err := app.reconcileRuntime("characterization", false)
			if test.wantErr == nil {
				if err != nil {
					t.Fatalf("reconcile error = %v", err)
				}
			} else if !test.wantErr(err) {
				t.Fatalf("reconcile error = %v, unexpected type", err)
			}
			net.mu.Lock()
			starts, stops := len(net.starts), net.stopCount
			net.mu.Unlock()
			if starts != test.wantStarts || stops != test.wantStops || len(dp.fullSyncs) != test.wantFullSyncs {
				t.Fatalf("runtime actions starts=%d stops=%d full_syncs=%d, want %d/%d/%d", starts, stops, len(dp.fullSyncs), test.wantStarts, test.wantStops, test.wantFullSyncs)
			}
		})
	}
}
