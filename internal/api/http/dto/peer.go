package dto

import (
	"time"

	"github.com/touken928/wirehub/internal/service"
)

type PeerResponse struct {
	ID            uint      `json:"id"`
	Name          string    `json:"name"`
	PublicKey     string    `json:"public_key"`
	WGIP          string    `json:"wg_ip"`
	GroupID       uint      `json:"group_id"`
	Enabled       bool      `json:"enabled"`
	DNSName       string    `json:"dns_name"`
	LastHandshake int64     `json:"last_handshake"`
	RxBytes       int64     `json:"rx_bytes"`
	TxBytes       int64     `json:"tx_bytes"`
	CreatedAt     time.Time `json:"created_at"`
	FQDN          string    `json:"fqdn"`
	GroupName     string    `json:"group_name,omitempty"`
}

func ToPeerResponse(p service.PeerView) PeerResponse {
	return PeerResponse{
		ID: p.ID, Name: p.Name, PublicKey: p.PublicKey, WGIP: p.WGIP, GroupID: p.GroupID,
		Enabled: p.Enabled, DNSName: p.DNSName, LastHandshake: p.LastHandshake,
		RxBytes: p.RxBytes, TxBytes: p.TxBytes, CreatedAt: p.CreatedAt,
		FQDN: p.FQDN, GroupName: p.GroupName,
	}
}

func ToPeerResponses(peers []service.PeerView) []PeerResponse {
	out := make([]PeerResponse, 0, len(peers))
	for _, peer := range peers {
		out = append(out, ToPeerResponse(peer))
	}
	return out
}
