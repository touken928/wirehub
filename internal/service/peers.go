package service

import (
	"errors"

	"github.com/touken928/wirehub/internal/domain/client"
	domainpeer "github.com/touken928/wirehub/internal/domain/peer"
	"github.com/touken928/wirehub/internal/repo"
)

// CreatePeer provisions a peer in the database and on the live network stack when running.
func (a *App) CreatePeer(in CreatePeerInput) (PeerView, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.createPeer(in)
}

func (a *App) createPeer(in CreatePeerInput) (PeerView, error) {
	slug, err := domainpeer.ValidateHostname(in.Name)
	if err != nil {
		return PeerView{}, err
	}

	group, err := a.store.GetGroup(in.GroupID)
	if err != nil {
		return PeerView{}, ErrGroupNotFound
	}

	existing, _ := a.store.ListPeers()
	for _, p := range existing {
		if p.Name == slug {
			return PeerView{}, ErrHostnameExists
		}
	}

	settings, err := a.store.GetSettings()
	if err != nil {
		return PeerView{}, err
	}

	priv, pub, err := a.keyGenerator()
	if err != nil {
		return PeerView{}, err
	}

	peer := &repo.Peer{
		Name:       slug,
		PublicKey:  pub,
		PrivateKey: priv,
		GroupID:    in.GroupID,
		Enabled:    true,
		DNSName:    slug,
	}

	if err := a.store.CreatePeerWithAllocatedIP(peer, settings.WGSubnet, settings.HubIP, settings.DNSIP); err != nil {
		if errors.Is(err, repo.ErrIPAllocationUnavailable) {
			return PeerView{}, ErrPeerIPUnavailable
		}
		return PeerView{}, err
	}
	if err := a.reconcileRuntime("peer creation", false); err != nil {
		return PeerView{}, err
	}
	return peerView(*peer, group.Name), nil
}

// sentinel errors for peer operations
var (
	ErrPeerNotFound      = errors.New("peer not found")
	ErrGroupNotFound     = errors.New("group not found")
	ErrHostnameExists    = errors.New("hostname already exists")
	ErrPeerIPUnavailable = errors.New("no peer IP available in subnet")
)

// UpdatePeerFields atomically renames and/or moves a peer. Nil fields are left unchanged.
func (a *App) UpdatePeerFields(id uint, in UpdatePeerInput) (PeerView, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.updatePeerFields(id, in)
}

func (a *App) updatePeerFields(id uint, in UpdatePeerInput) (PeerView, error) {
	peer, err := a.store.GetPeer(id)
	if err != nil {
		return PeerView{}, ErrPeerNotFound
	}
	var groupName string
	if in.GroupID != nil {
		group, err := a.store.GetGroup(*in.GroupID)
		if err != nil {
			return PeerView{}, ErrGroupNotFound
		}
		groupName = group.Name
	} else if group, err := a.store.GetGroup(peer.GroupID); err == nil {
		groupName = group.Name
	}
	changed := false
	if in.Name != nil {
		slug, err := domainpeer.ValidateHostname(*in.Name)
		if err != nil {
			return PeerView{}, err
		}
		if peer.Name != slug {
			existing, _ := a.store.ListPeers()
			for _, p := range existing {
				if p.ID != id && p.Name == slug {
					return PeerView{}, ErrHostnameExists
				}
			}
			peer.Name = slug
			peer.DNSName = slug
			changed = true
		}
	}
	if in.GroupID != nil && peer.GroupID != *in.GroupID {
		peer.GroupID = *in.GroupID
		changed = true
	}
	if !changed {
		return peerView(*peer, groupName), nil
	}
	if err := a.store.UpdatePeer(peer); err != nil {
		return PeerView{}, err
	}
	if in.Name != nil {
		if err := a.ensurePeerDNSRecord(peer.ID, peer.DNSName, peer.WGIP); err != nil {
			return PeerView{}, err
		}
	}
	if err := a.reconcileRuntime("peer update", false); err != nil {
		return PeerView{}, err
	}
	return peerView(*peer, groupName), nil
}

// DeletePeer removes a peer from the database and live network.
func (a *App) DeletePeer(peerID uint) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	_, err := a.store.GetPeer(peerID)
	if err != nil {
		return ErrPeerNotFound
	}
	if err := a.store.DeleteDNSByPeerID(peerID); err != nil {
		return err
	}
	if err := a.store.DeletePeer(peerID); err != nil {
		return err
	}
	if err := a.reconcileRuntime("peer deletion", false); err != nil {
		return err
	}
	return nil
}

// TogglePeer enables or disables a peer on the live network.
func (a *App) TogglePeer(peerID uint) (PeerView, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.togglePeer(peerID)
}

func (a *App) togglePeer(peerID uint) (PeerView, error) {
	peer, err := a.store.GetPeer(peerID)
	if err != nil {
		return PeerView{}, ErrPeerNotFound
	}
	groupName := ""
	if group, err := a.store.GetGroup(peer.GroupID); err == nil {
		groupName = group.Name
	}
	peer.Enabled = !peer.Enabled
	if err := a.store.UpdatePeer(peer); err != nil {
		return PeerView{}, err
	}
	if err := a.reconcileRuntime("peer toggle", false); err != nil {
		return PeerView{}, err
	}
	return peerView(*peer, groupName), nil
}

// ClientConfig renders the WireGuard client config for a peer.
func (a *App) ClientConfig(peerID uint) (ClientConfigResult, error) {
	peer, err := a.store.GetPeer(peerID)
	if err != nil {
		return ClientConfigResult{}, ErrPeerNotFound
	}
	settings, err := a.store.GetSettings()
	if err != nil {
		return ClientConfigResult{}, err
	}
	config, err := client.BuildClientConfig(client.ClientConfigInput{
		Endpoint:        settings.Endpoint,
		ListenPort:      settings.ListenPort,
		ServerPublicKey: settings.ServerPublicKey,
		AllowedSubnet:   settings.WGSubnet,
		ClientDNS:       settings.ClientDNS(),
		PeerPrivateKey:  peer.PrivateKey,
		PeerAddress:     peer.WGIP,
	})
	if err != nil {
		return ClientConfigResult{}, err
	}
	return ClientConfigResult{Config: config, Filename: peer.Name + ".conf"}, nil
}
