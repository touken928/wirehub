package dto

import (
	"time"

	"github.com/touken928/wirehub/internal/service"
)

type GroupResponse struct {
	ID              uint    `json:"id"`
	Name            string  `json:"name"`
	PosX            float64 `json:"pos_x"`
	PosY            float64 `json:"pos_y"`
	AllowIntraGroup bool    `json:"allow_intra_group"`
	MemberCount     int64   `json:"MemberCount"`
}

func ToGroupResponse(v service.GroupView) GroupResponse {
	return GroupResponse{ID: v.ID, Name: v.Name, PosX: v.PosX, PosY: v.PosY, AllowIntraGroup: v.AllowIntraGroup, MemberCount: v.MemberCount}
}

type GroupPeerResponse struct {
	ID            uint   `json:"id"`
	Name          string `json:"name"`
	PublicKey     string `json:"public_key"`
	WGIP          string `json:"wg_ip"`
	GroupID       uint   `json:"group_id"`
	Enabled       bool   `json:"enabled"`
	DNSName       string `json:"dns_name"`
	LastHandshake int64  `json:"last_handshake"`
	RxBytes       int64  `json:"rx_bytes"`
	TxBytes       int64  `json:"tx_bytes"`
	CreatedAt     string `json:"created_at"`
	FQDN          string `json:"fqdn"`
}

type GroupGraphGroupResponse struct {
	ID              uint                `json:"id"`
	Name            string              `json:"name"`
	PosX            float64             `json:"pos_x"`
	PosY            float64             `json:"pos_y"`
	AllowIntraGroup bool                `json:"allow_intra_group"`
	MemberCount     int64               `json:"member_count"`
	Peers           []GroupPeerResponse `json:"peers"`
}

type GroupLinkResponse struct {
	ID            uint `json:"id"`
	FromGroupID   uint `json:"from_group_id"`
	ToGroupID     uint `json:"to_group_id"`
	Bidirectional bool `json:"bidirectional"`
}

type GroupGraphResponse struct {
	Groups []GroupGraphGroupResponse `json:"groups"`
	Links  []GroupLinkResponse       `json:"links"`
}

func ToGroupGraphResponse(v service.GroupGraphView) GroupGraphResponse {
	groups := make([]GroupGraphGroupResponse, 0, len(v.Groups))
	for _, group := range v.Groups {
		var peers []GroupPeerResponse
		if group.Peers != nil {
			peers = make([]GroupPeerResponse, 0, len(group.Peers))
			for _, peer := range group.Peers {
				peers = append(peers, GroupPeerResponse{
					ID: peer.ID, Name: peer.Name, PublicKey: peer.PublicKey, WGIP: peer.WGIP, GroupID: peer.GroupID,
					Enabled: peer.Enabled, DNSName: peer.DNSName, LastHandshake: peer.LastHandshake,
					RxBytes: peer.RxBytes, TxBytes: peer.TxBytes, CreatedAt: peer.CreatedAt.Format(time.RFC3339Nano),
					FQDN: peer.FQDN,
				})
			}
		}
		groups = append(groups, GroupGraphGroupResponse{ID: group.ID, Name: group.Name, PosX: group.PosX, PosY: group.PosY, AllowIntraGroup: group.AllowIntraGroup, MemberCount: group.MemberCount, Peers: peers})
	}
	links := make([]GroupLinkResponse, 0, len(v.Links))
	for _, link := range v.Links {
		links = append(links, GroupLinkResponse{ID: link.ID, FromGroupID: link.FromGroupID, ToGroupID: link.ToGroupID, Bidirectional: link.Bidirectional})
	}
	return GroupGraphResponse{Groups: groups, Links: links}
}
