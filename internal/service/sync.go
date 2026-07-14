package service

import (
	"fmt"
	"github.com/touken928/wirehub/internal/config"
	"github.com/touken928/wirehub/internal/domain/policy"
	"github.com/touken928/wirehub/internal/domain/runtime"
	"github.com/touken928/wirehub/internal/repo"
)

func (a *App) loadSyncBundle() (runtime.SyncBundle, error) {
	settings, err := a.store.GetSettings()
	if err != nil {
		return runtime.SyncBundle{}, err
	}
	peers, err := a.store.ListPeers()
	if err != nil {
		return runtime.SyncBundle{}, err
	}
	links, err := a.store.ListGroupLinks()
	if err != nil {
		return runtime.SyncBundle{}, err
	}
	groups, err := a.store.ListGroups()
	if err != nil {
		return runtime.SyncBundle{}, err
	}
	forwards, err := a.store.ListPortForwards()
	if err != nil {
		return runtime.SyncBundle{}, err
	}
	mapDetails, err := a.store.ListMapDetails()
	if err != nil {
		return runtime.SyncBundle{}, err
	}

	accessSpec, err := a.buildAccessPolicySpecFrom(peerEndpoints(peers), groupLinkPairs(links), groupAccessList(groups))
	if err != nil {
		return runtime.SyncBundle{}, err
	}

	mtu := settings.MTU
	if mtu == 0 {
		mtu = config.DefaultMTU
	}
	statusInterval := settings.StatusInterval
	if statusInterval == 0 {
		statusInterval = config.DefaultStatusInterval
	}

	wgPeers := make([]runtime.WGPeer, len(peers))
	for i, p := range peers {
		wgPeers[i] = runtime.WGPeer{
			ID:        p.ID,
			PublicKey: p.PublicKey,
			WGIP:      p.WGIP,
			DNSName:   p.DNSName,
			GroupID:   p.GroupID,
			Enabled:   p.Enabled,
		}
	}
	fwdRules := make([]runtime.ForwardRule, len(forwards))
	for i, r := range forwards {
		fwdRules[i] = runtime.ForwardRule{
			ID:         r.ID,
			ListenPort: r.ListenPort,
			Protocol:   r.Protocol,
			TargetHost: r.TargetHost,
			TargetPort: r.TargetPort,
		}
	}
	mapRules := make([]runtime.MapRule, len(mapDetails))
	for i, d := range mapDetails {
		mapRules[i] = runtime.MapRule{
			ID:              d.ID,
			Slug:            d.Slug,
			TargetHost:      d.TargetHost,
			VirtualIP:       d.VirtualIP,
			AllowedGroupIDs: policy.AllowedGroupIDSet(d.AllowedGroups),
		}
	}

	netSettings := runtime.NetworkSettings{
		HubIP:            settings.HubIP,
		DNSIP:            settings.DNSIP,
		WGSubnet:         settings.WGSubnet,
		ServerPrivateKey: settings.ServerPrivateKey,
		MTU:              mtu,
		ListenPort:       settings.ListenPort,
		StatusInterval:   statusInterval,
		UpstreamDNS:      settings.UpstreamDNSResolvers(),
	}

	return runtime.SyncBundle{
		Settings: netSettings,
		Peers:    wgPeers,
		Policy:   accessSpec,
		Forwards: fwdRules,
		Maps:     mapRules,
		DNS:      runtime.BuildDNSCatalog(settings.HubIP, wgPeers, mapRules),
	}, nil
}

// ensurePeerDNSRecord creates or refreshes authoritative DNS in the database.
func (a *App) ensurePeerDNSRecord(peerID uint, dnsName, wgIP string) error {
	slug := dnsName
	if slug == "" {
		return fmt.Errorf("peer DNS name is required")
	}
	_ = a.store.DeleteDNSByPeerID(peerID)
	record := &repo.DNSRecord{
		Hostname: slug,
		IP:       wgIP,
		PeerID:   &peerID,
		Manual:   false,
	}
	return a.store.CreateDNSRecord(record)
}
