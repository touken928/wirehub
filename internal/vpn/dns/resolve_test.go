package dns

import (
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/touken928/wirehub/internal/domain/runtime"
)

func TestUpstreamSnapshotIsImmutableAndRaceFree(t *testing.T) {
	s := NewServer("100.127.0.1", []string{"1.1.1.1"})
	input := []string{"9.9.9.9"}
	s.SetUpstream(input)
	input[0] = "8.8.8.8"
	if got := s.upstreamSnapshot(); len(got) != 1 || got[0] != "9.9.9.9" {
		t.Fatalf("upstream snapshot = %v", got)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.SetUpstream([]string{"192.0.2." + string(rune('1'+i))})
				_ = s.upstreamSnapshot()
			}
		}(i)
	}
	wg.Wait()
}

func TestResolveHostExternalViaUpstream(t *testing.T) {
	s := NewServer("100.127.0.1", []string{"114.114.114.114"})
	s.UpdateDNS(runtime.DNSCatalog{HubIP: "100.127.0.1"}, nil)
	addr, err := s.ResolveHost("example.com")
	if err != nil {
		t.Skipf("upstream dns unavailable: %v", err)
	}
	if !addr.Is4() {
		t.Fatalf("expected IPv4, got %s", addr)
	}
}

func TestResolveHostExternalWithoutUpstream(t *testing.T) {
	s := NewServer("100.127.0.1", nil)
	_, err := s.ResolveHost("example.com")
	if err == nil {
		t.Fatal("expected error without upstream DNS")
	}
	if !strings.Contains(err.Error(), "upstream") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveHostUnreachableUpstream(t *testing.T) {
	s := NewServer("100.127.0.1", []string{"203.0.113.53"})
	_, err := s.ResolveHost("example.com")
	if err == nil {
		t.Skip("upstream unexpectedly reachable in this environment")
	}
	if !strings.Contains(err.Error(), "upstream") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveHostExternalViaUpstreamNotVPNLoop(t *testing.T) {
	s := NewServer("100.127.0.1", []string{"100.127.0.1", "114.114.114.114"})
	addr, err := s.ResolveHost("example.com")
	if err != nil {
		t.Skipf("upstream dns unavailable: %v", err)
	}
	if addr == (netip.Addr{}) {
		t.Fatal("expected address")
	}
}
