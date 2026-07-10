package ingress

import (
	"net/netip"
	"testing"
)

func TestMapUDPGenerationGuardsProtectReappliedEntries(t *testing.T) {
	key := flowKey{
		client:     netip.MustParseAddr("100.127.0.2"),
		server:     netip.MustParseAddr("100.127.0.10"),
		clientPort: 40123,
		serverPort: 19053,
		proto:      protoUDP,
	}
	m := &MapProxy{
		udpSessions: make(map[flowKey]*udpMapSession),
		udpPending:  make(map[flowKey]uint64),
	}
	old := &udpMapSession{generation: 1}
	current := &udpMapSession{generation: 2}
	m.udpSessions[key] = current
	m.udpPending[key] = 2

	// Completion from the old generation must not remove the current pending
	// creation or active session installed by a reapply.
	m.deleteUDPPending(key, old.generation)
	m.deleteUDPSession(key, old)
	if got := m.udpPending[key]; got != 2 {
		t.Fatalf("pending generation = %d, want 2", got)
	}
	if got := m.udpSessions[key]; got != current {
		t.Fatal("old session cleanup removed current session")
	}

	m.deleteUDPPending(key, current.generation)
	m.deleteUDPSession(key, current)
	if _, ok := m.udpPending[key]; ok {
		t.Fatal("current pending session was not removed")
	}
	if _, ok := m.udpSessions[key]; ok {
		t.Fatal("current session was not removed")
	}
}
