package ingress

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/touken928/wirehub/internal/domain/policy"
	"github.com/touken928/wirehub/internal/domain/runtime"
	vpnnetstack "github.com/touken928/wirehub/internal/vpn/netstack"
	wgnetstack "golang.zx2c4.com/wireguard/tun/netstack"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const mapNIC tcpip.NICID = 1

// MapRule is a runtime virtual-IP map mapping (TCP/UDP, port-preserving).
type MapRule struct {
	ID              uint
	Slug            string
	TargetHost      string
	VirtualIP       netip.Addr
	AllowedGroupIDs map[uint]struct{}
}

type udpMapSession struct {
	client     *gonet.UDPConn
	backend    net.Conn
	lastActive time.Time
	generation uint64
}

// MapProxy terminates TCP/UDP to map virtual IPs and dials map targets.
type MapProxy struct {
	tnet      *wgnetstack.Net
	vpnSubnet *net.IPNet
	resolver  HostResolver

	mu          sync.Mutex
	peerGroup   map[netip.Addr]uint
	rules       map[netip.Addr]*MapRule
	slugByIP    map[netip.Addr]string
	cancel      context.CancelFunc
	ctx         context.Context
	generation  uint64
	tcpFwd      *tcp.Forwarder
	udpFwd      *udp.Forwarder
	udpMu       sync.Mutex
	udpSessions map[flowKey]*udpMapSession
	udpPending  map[flowKey]uint64
}

func NewMapProxy(tnet *wgnetstack.Net, vpnSubnet string, resolver HostResolver) (*MapProxy, error) {
	subnet, err := parseVPNSubnet(vpnSubnet)
	if err != nil {
		return nil, err
	}
	return &MapProxy{
		tnet:        tnet,
		vpnSubnet:   subnet,
		resolver:    resolver,
		rules:       make(map[netip.Addr]*MapRule),
		slugByIP:    make(map[netip.Addr]string),
		peerGroup:   make(map[netip.Addr]uint),
		udpSessions: make(map[flowKey]*udpMapSession),
		udpPending:  make(map[flowKey]uint64),
	}, nil
}

// SetPeerGroups refreshes WG IP → group lookup used for map ACL checks.
func (m *MapProxy) SetPeerGroups(peers []runtime.WGPeer) {
	pg := make(map[netip.Addr]uint, len(peers))
	for _, p := range peers {
		if !p.Enabled || p.GroupID == 0 {
			continue
		}
		ip, err := netip.ParseAddr(p.WGIP)
		if err != nil {
			continue
		}
		pg[ip] = p.GroupID
	}
	m.mu.Lock()
	m.peerGroup = pg
	m.mu.Unlock()
}

func (m *MapProxy) peerAllowed(rule *MapRule, peerIP netip.Addr) bool {
	if rule == nil || len(rule.AllowedGroupIDs) == 0 {
		return false
	}
	m.mu.Lock()
	gid, ok := m.peerGroup[peerIP]
	m.mu.Unlock()
	if !ok {
		return false
	}
	return policy.GroupInAllowedSet(rule.AllowedGroupIDs, gid)
}

func (m *MapProxy) Apply(rules []MapRule, peers []runtime.WGPeer) error {
	stk, err := vpnnetstack.StackFromNet(m.tnet)
	if err != nil {
		return err
	}
	newRules := make(map[netip.Addr]*MapRule, len(rules))
	newSlugs := make(map[netip.Addr]string, len(rules))

	for i := range rules {
		rule := rules[i]
		if err := ensureMapAddress(stk, rule.VirtualIP); err != nil {
			return fmt.Errorf("map %s: add address %s: %w", rule.Slug, rule.VirtualIP, err)
		}
		newRules[rule.VirtualIP] = &rules[i]
		newSlugs[rule.VirtualIP] = rule.Slug
	}

	ctx, cancel := context.WithCancel(context.Background())
	pg := make(map[netip.Addr]uint, len(peers))
	for _, p := range peers {
		if !p.Enabled || p.GroupID == 0 {
			continue
		}
		ip, parseErr := netip.ParseAddr(p.WGIP)
		if parseErr == nil {
			pg[ip] = p.GroupID
		}
	}

	m.mu.Lock()
	oldCancel := m.cancel
	m.generation++
	m.cancel = cancel
	m.ctx = ctx
	m.closeUDPSessions()
	m.rules = newRules
	m.slugByIP = newSlugs
	m.peerGroup = pg
	m.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}

	m.mu.Lock()
	if m.tcpFwd == nil {
		m.tcpFwd = tcp.NewForwarder(stk, 0, 512, func(req *tcp.ForwarderRequest) {
			m.handleTCPForwarderRequest(m.currentContext(), req)
		})
		stk.SetTransportProtocolHandler(tcp.ProtocolNumber, m.tcpFwd.HandlePacket)
	}
	if m.udpFwd == nil {
		m.udpFwd = udp.NewForwarder(stk, func(req *udp.ForwarderRequest) {
			m.handleUDPForwarderRequest(m.currentContext(), req)
		})
		stk.SetTransportProtocolHandler(udp.ProtocolNumber, m.udpFwd.HandlePacket)
	}
	m.mu.Unlock()

	return nil
}

func (m *MapProxy) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.ctx = nil
	m.generation++
	m.closeUDPSessions()
	m.rules = make(map[netip.Addr]*MapRule)
	m.slugByIP = make(map[netip.Addr]string)
}

func (m *MapProxy) currentContext() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

func (m *MapProxy) closeUDPSessions() {
	m.udpMu.Lock()
	defer m.udpMu.Unlock()
	for _, sess := range m.udpSessions {
		_ = sess.backend.Close()
		_ = sess.client.Close()
	}
	m.udpSessions = make(map[flowKey]*udpMapSession)
	m.udpPending = make(map[flowKey]uint64)
}

// EnsureStackMapAddresses adds map virtual IPs to the hub gVisor stack (idempotent).
func EnsureStackMapAddresses(stk *stack.Stack, addrs []netip.Addr) error {
	for _, addr := range addrs {
		if err := ensureMapAddress(stk, addr); err != nil {
			return err
		}
	}
	return nil
}

func ensureMapAddress(stk *stack.Stack, addr netip.Addr) error {
	if !addr.IsValid() || !addr.Is4() {
		return nil
	}
	protoAddr := tcpip.ProtocolAddress{
		Protocol:          ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddrFromSlice(addr.AsSlice()).WithPrefix(),
	}
	if err := stk.AddProtocolAddress(mapNIC, protoAddr, stack.AddressProperties{}); err != nil {
		if _, ok := err.(*tcpip.ErrDuplicateAddress); ok {
			return nil
		}
		return fmt.Errorf("%v", err)
	}
	return nil
}

func (m *MapProxy) handleTCPForwarderRequest(ctx context.Context, req *tcp.ForwarderRequest) {
	id := req.ID()
	localIP, ok := netip.AddrFromSlice(id.LocalAddress.AsSlice())
	if !ok {
		return
	}

	m.mu.Lock()
	rule := m.rules[localIP]
	m.mu.Unlock()
	if rule == nil {
		req.Complete(true)
		return
	}

	remoteIP, ok := netip.AddrFromSlice(id.RemoteAddress.AsSlice())
	if !ok {
		req.Complete(true)
		return
	}
	if !m.peerAllowed(rule, remoteIP) {
		req.Complete(true)
		return
	}

	var wq waiter.Queue
	ep, tcpErr := req.CreateEndpoint(&wq)
	if tcpErr != nil {
		req.Complete(true)
		return
	}
	req.Complete(false)

	go m.proxyTCP(ctx, rule, gonet.NewTCPConn(&wq, ep), id.LocalPort)
}

func (m *MapProxy) handleUDPForwarderRequest(ctx context.Context, req *udp.ForwarderRequest) {
	id := req.ID()
	localIP, ok := netip.AddrFromSlice(id.LocalAddress.AsSlice())
	if !ok {
		return
	}

	m.mu.Lock()
	rule := m.rules[localIP]
	generation := m.generation
	m.mu.Unlock()
	if rule == nil {
		return
	}

	remoteIP, ok := netip.AddrFromSlice(id.RemoteAddress.AsSlice())
	if !ok {
		return
	}
	if !m.peerAllowed(rule, remoteIP) {
		return
	}

	key := flowKey{
		client:     remoteIP,
		server:     localIP,
		clientPort: id.RemotePort,
		serverPort: id.LocalPort,
		proto:      protoUDP,
	}

	// Keep the map lifecycle lock while reserving the flow so Apply cannot
	// advance the generation between this check and pending publication.
	m.mu.Lock()
	if m.generation != generation || m.rules[localIP] != rule {
		m.mu.Unlock()
		return
	}
	m.udpMu.Lock()
	m.mu.Unlock()
	if _, exists := m.udpSessions[key]; exists {
		m.udpMu.Unlock()
		return
	}
	if _, pending := m.udpPending[key]; pending {
		m.udpMu.Unlock()
		return
	}
	m.udpPending[key] = generation
	m.udpMu.Unlock()

	var wq waiter.Queue
	ep, udpErr := req.CreateEndpoint(&wq)
	if udpErr != nil {
		m.deleteUDPPending(key, generation)
		return
	}
	client := gonet.NewUDPConn(&wq, ep)
	go m.createMapUDPSession(ctx, generation, key, id.LocalPort, rule, client)
}

func (m *MapProxy) createMapUDPSession(ctx context.Context, generation uint64, key flowKey, localPort uint16, rule *MapRule, client *gonet.UDPConn) {
	addrs, err := m.resolver.ResolveForwardAddrs(rule.TargetHost)
	if err != nil {
		log.Printf("map %s resolve %q: %v", rule.Slug, rule.TargetHost, err)
		_ = client.Close()
		m.deleteUDPPending(key, generation)
		return
	}
	var backend net.Conn
	for _, addr := range addrs {
		target := netip.AddrPortFrom(addr, localPort)
		backend, err = m.dialTarget(ctx, "udp", target)
		if err == nil {
			break
		}
		log.Printf("map %s dial %s: %v", rule.Slug, target, err)
	}
	if backend == nil {
		_ = client.Close()
		m.deleteUDPPending(key, generation)
		return
	}
	sess := &udpMapSession{client: client, backend: backend, lastActive: time.Now(), generation: generation}
	m.udpMu.Lock()
	if m.udpPending[key] != generation {
		m.udpMu.Unlock()
		_ = backend.Close()
		_ = client.Close()
		return
	}
	delete(m.udpPending, key)
	if ctx.Err() != nil {
		m.udpMu.Unlock()
		_ = backend.Close()
		_ = client.Close()
		return
	}
	if _, exists := m.udpSessions[key]; exists {
		m.udpMu.Unlock()
		_ = backend.Close()
		_ = client.Close()
		return
	}
	m.udpSessions[key] = sess
	m.udpMu.Unlock()
	go m.mapUDPClientToBackend(ctx, rule, sess)
	go m.mapUDPBackendToClient(ctx, key, sess)
}

func (m *MapProxy) deleteUDPPending(key flowKey, generation uint64) {
	m.udpMu.Lock()
	if m.udpPending[key] == generation {
		delete(m.udpPending, key)
	}
	m.udpMu.Unlock()
}

func (m *MapProxy) mapUDPClientToBackend(ctx context.Context, rule *MapRule, sess *udpMapSession) {
	buf := make([]byte, 64*1024)
	for {
		if ctx.Err() != nil {
			return
		}
		_ = sess.client.SetReadDeadline(time.Now().Add(SessionIdle))
		n, err := sess.client.Read(buf)
		if err != nil {
			return
		}
		if _, err := sess.backend.Write(buf[:n]); err != nil {
			log.Printf("map %s client->backend: %v", rule.Slug, err)
			return
		}
		m.udpMu.Lock()
		sess.lastActive = time.Now()
		m.udpMu.Unlock()
	}
}

func (m *MapProxy) mapUDPBackendToClient(ctx context.Context, key flowKey, sess *udpMapSession) {
	defer func() {
		_ = sess.backend.Close()
		_ = sess.client.Close()
		m.deleteUDPSession(key, sess)
	}()
	buf := make([]byte, 64*1024)
	for {
		if ctx.Err() != nil {
			return
		}
		_ = sess.backend.SetReadDeadline(time.Now().Add(SessionIdle))
		n, err := sess.backend.Read(buf)
		if err != nil {
			return
		}
		if _, err := sess.client.Write(buf[:n]); err != nil {
			return
		}
		m.udpMu.Lock()
		sess.lastActive = time.Now()
		m.udpMu.Unlock()
	}
}

func (m *MapProxy) deleteUDPSession(key flowKey, sess *udpMapSession) {
	m.udpMu.Lock()
	if current := m.udpSessions[key]; current == sess && current.generation == sess.generation {
		delete(m.udpSessions, key)
	}
	m.udpMu.Unlock()
}

func (m *MapProxy) proxyTCP(ctx context.Context, rule *MapRule, client *gonet.TCPConn, localPort uint16) {
	defer client.Close()

	addrs, err := m.resolver.ResolveForwardAddrs(rule.TargetHost)
	if err != nil {
		log.Printf("map %s resolve %q: %v", rule.Slug, rule.TargetHost, err)
		return
	}
	var remote net.Conn
	for _, addr := range addrs {
		target := netip.AddrPortFrom(addr, localPort)
		remote, err = m.dialTarget(ctx, "tcp", target)
		if err == nil {
			break
		}
		log.Printf("map %s dial %s: %v", rule.Slug, target, err)
	}
	if remote == nil {
		return
	}
	defer remote.Close()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remote, client)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, remote)
		done <- struct{}{}
	}()
	select {
	case <-ctx.Done():
	case <-done:
		_ = remote.Close()
		<-done
	}
}

func (m *MapProxy) dialTarget(ctx context.Context, network string, target netip.AddrPort) (net.Conn, error) {
	fp := &ForwardProxy{
		tnet:      m.tnet,
		vpnSubnet: m.vpnSubnet,
		resolver:  m.resolver,
	}
	return fp.dialTarget(ctx, network, target)
}

// MapVIPAddrs collects virtual IPs from map rules.
func MapVIPAddrs(rules []MapRule) []netip.Addr {
	out := make([]netip.Addr, 0, len(rules))
	for _, r := range rules {
		if r.VirtualIP.IsValid() {
			out = append(out, r.VirtualIP)
		}
	}
	return out
}

// ParseMapVIP parses map virtual IP strings for stack startup.
func ParseMapVIP(ips []string) []netip.Addr {
	out := make([]netip.Addr, 0, len(ips))
	for _, s := range ips {
		addr, err := netip.ParseAddr(s)
		if err == nil && addr.Is4() {
			out = append(out, addr)
		}
	}
	return out
}
