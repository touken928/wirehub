package dto

import "github.com/touken928/wirehub/internal/service"

type PortForwardResponse struct {
	ID            uint   `json:"id"`
	Name          string `json:"name"`
	ListenPort    int    `json:"listen_port"`
	Protocol      string `json:"protocol"`
	TargetHost    string `json:"target_host"`
	TargetPort    int    `json:"target_port"`
	TargetDisplay string `json:"target_display"`
}

func ToPortForwardResponse(f service.PortForwardView) PortForwardResponse {
	return PortForwardResponse{
		ID: f.ID, Name: f.Name, ListenPort: f.ListenPort, Protocol: f.Protocol,
		TargetHost: f.TargetHost, TargetPort: f.TargetPort, TargetDisplay: f.TargetDisplay,
	}
}
