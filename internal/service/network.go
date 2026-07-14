package service

import (
	dompolicy "github.com/touken928/wirehub/internal/domain/policy"
	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
)

// NetworkRuntime controls VPN stack lifecycle.
type networkRuntime interface {
	Start(bundle domainruntime.SyncBundle) error
	Stop() error
	SetDNSUpstream(upstream []string)
}

// Dataplane is the live VPN data plane as consumed by service.
// Keep this interface service-owned so control-plane code does not depend on
// concrete vpn/runtime types outside the lifecycle bridge.
type Dataplane interface {
	ApplyPolicy(spec dompolicy.AccessPolicySpec) error
	UpdateDNS(catalog domainruntime.DNSCatalog, peers []domainruntime.WGPeer) error
	FullSync(bundle domainruntime.SyncBundle) error
	GetStats() (map[string]domainruntime.PeerStats, error)
}
