package service

import (
	"time"

	domainpeer "github.com/touken928/wirehub/internal/domain/peer"
	"github.com/touken928/wirehub/internal/repo"
)

// PeerView is the public peer representation used by HTTP and status clients.
// PrivateKey is intentionally absent; client configuration is generated separately.
type PeerView struct {
	ID            uint
	Name          string
	PublicKey     string
	WGIP          string
	GroupID       uint
	Enabled       bool
	DNSName       string
	LastHandshake int64
	RxBytes       int64
	TxBytes       int64
	CreatedAt     time.Time
	FQDN          string
	GroupName     string
}

type CreatePeerInput struct {
	Name    string
	GroupID uint
}

type UpdatePeerInput struct {
	Name    *string
	GroupID *uint
}

type ClientConfigResult struct {
	Config   string
	Filename string
}

// ListPeers returns all peers from persistence.
func (a *App) ListPeers() ([]PeerView, error) {
	peers, err := a.store.ListPeers()
	if err != nil {
		return nil, err
	}
	groupNames := a.getGroupNameMap()
	out := make([]PeerView, 0, len(peers))
	for _, peer := range peers {
		out = append(out, peerView(peer, groupNames[peer.GroupID]))
	}
	return out, nil
}

func peerView(peer repo.Peer, groupName string) PeerView {
	return PeerView{
		ID: peer.ID, Name: peer.Name, PublicKey: peer.PublicKey, WGIP: peer.WGIP,
		GroupID: peer.GroupID, Enabled: peer.Enabled, DNSName: peer.DNSName,
		LastHandshake: peer.LastHandshake, RxBytes: peer.RxBytes, TxBytes: peer.TxBytes,
		CreatedAt: peer.CreatedAt, FQDN: domainpeer.PeerFQDN(peer.Name), GroupName: groupName,
	}
}
