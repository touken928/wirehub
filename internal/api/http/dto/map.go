package dto

import (
	"github.com/touken928/wirehub/internal/service"
)

type MapResponse struct {
	ID              uint   `json:"id"`
	Name            string `json:"name"`
	Slug            string `json:"slug"`
	TargetHost      string `json:"target_host"`
	VirtualIP       string `json:"virtual_ip"`
	TargetDisplay   string `json:"target_display"`
	AllowedGroupIDs []uint `json:"allowed_group_ids"`
	FQDN            string `json:"fqdn"`
}

func ToMapResponse(d service.MapView) MapResponse {
	return MapResponse{
		ID: d.ID, Name: d.Name, Slug: d.Slug, TargetHost: d.TargetHost,
		VirtualIP: d.VirtualIP, TargetDisplay: d.TargetDisplay,
		AllowedGroupIDs: d.AllowedGroupIDs, FQDN: d.FQDN,
	}
}
