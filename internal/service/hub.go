package service

import (
	"errors"
	"sync"
	"time"

	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
	"github.com/touken928/wirehub/internal/repo"
	"github.com/touken928/wirehub/internal/vpn/tunnel"
)

var ErrNetworkUnavailable = errors.New("network runtime is not running")

// StatusPublisher receives a push after peer stats are polled from WireGuard.
type StatusPublisher interface {
	Publish()
}

// Hub manages VPN network lifecycle and dataplane access.
type Hub struct {
	app *App

	networkMu       sync.RWMutex
	network         NetworkRuntime
	dpMu            sync.RWMutex
	liveDP          Dataplane
	statusMu        sync.Mutex
	statusStop      chan struct{}
	statusDone      chan struct{}
	statusRunning   bool
	statusPublisher StatusPublisher
}

func (h *Hub) SetStatusPublisher(p StatusPublisher) {
	h.statusPublisher = p
}

func NewHub(a *App) *Hub {
	return &Hub{app: a}
}

func (h *Hub) SetNetworkRuntime(nc NetworkRuntime) {
	h.networkMu.Lock()
	h.network = nc
	h.networkMu.Unlock()
}

func (h *Hub) NetworkRuntime() NetworkRuntime {
	h.networkMu.RLock()
	defer h.networkMu.RUnlock()
	return h.network
}

func (h *Hub) dataplane() Dataplane {
	h.dpMu.RLock()
	defer h.dpMu.RUnlock()
	return h.liveDP
}

func (h *Hub) onStarted(dp Dataplane) {
	h.dpMu.Lock()
	h.liveDP = dp
	h.dpMu.Unlock()
	if bundle, err := h.app.loadSyncBundle(); err == nil {
		h.StartStatusPoller(bundle.Settings.StatusInterval)
	}
	_ = h.app.SyncAccessFilter()
}

func (h *Hub) onStopped() {
	h.StopStatusPoller()
	h.dpMu.Lock()
	h.liveDP = nil
	h.dpMu.Unlock()
}

// StartStatusPoller begins periodic peer-stats polling. It is idempotent:
// if a poller is already running, subsequent calls are no-ops.
// Non-positive intervals default to 1 second to avoid NewTicker panics.
func (h *Hub) StartStatusPoller(intervalSec int) {
	for {
		h.statusMu.Lock()
		if h.statusRunning {
			h.statusMu.Unlock()
			return
		}
		oldDone := h.statusDone
		if oldDone == nil {
			break
		}
		h.statusMu.Unlock()
		<-oldDone
	}
	defer h.statusMu.Unlock()
	if h.statusRunning {
		return
	}
	if intervalSec <= 0 {
		intervalSec = 1
	}
	h.statusRunning = true
	ch := make(chan struct{})
	done := make(chan struct{})
	h.statusStop = ch
	h.statusDone = done

	go func() {
		ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
		defer ticker.Stop()
		defer func() {
			close(done)
			h.statusMu.Lock()
			if h.statusDone == done {
				h.statusDone = nil
			}
			h.statusMu.Unlock()
		}()
		for {
			select {
			case <-ticker.C:
				h.pollPeerStats()
			case <-ch:
				return
			}
		}
	}()
}

// StopStatusPoller stops a running poller. It is safe to call multiple times
// and when no poller is running.
func (h *Hub) StopStatusPoller() {
	h.statusMu.Lock()
	if !h.statusRunning {
		done := h.statusDone
		h.statusMu.Unlock()
		if done != nil {
			<-done
		}
		return
	}
	stop := h.statusStop
	done := h.statusDone
	close(stop)
	h.statusStop = nil
	h.statusRunning = false
	h.statusMu.Unlock()
	// Stack.Stop invokes this callback before taking the stack lock, so waiting
	// here cannot invert the stack/stats lock order. This also guarantees that
	// a restart cannot overlap the previous poller's final publication.
	<-done
}

// SyncPortForwards pushes port-forward rules to the live network stack.
// The network runtime is captured under the read lock to reduce pointer-after-unlock hazards.
func (h *Hub) SyncPortForwards() error {
	h.networkMu.RLock()
	nc := h.network
	if nc == nil {
		h.networkMu.RUnlock()
		return nil
	}
	err := nc.SyncPortForwards()
	h.networkMu.RUnlock()
	return err
}

// SyncMaps pushes service-map state to the live network stack and refreshes ACL.
func (h *Hub) SyncMaps() error {
	var err error
	h.networkMu.RLock()
	nc := h.network
	if nc != nil {
		err = nc.SyncMaps()
	}
	h.networkMu.RUnlock()
	if err != nil {
		return err
	}
	return h.app.SyncAccessFilter()
}

// SyncRuntimeBundle loads the authoritative database bundle and publishes it
// as one runtime snapshot after a persisted mutation.
func (h *Hub) SyncRuntimeBundle() error {
	bundle, err := h.app.LoadSyncBundle()
	if err != nil {
		return err
	}
	return h.syncRuntimeBundle(bundle)
}

func (h *Hub) syncRuntimeBundle(bundle domainruntime.SyncBundle) error {
	h.dpMu.RLock()
	dp := h.liveDP
	h.dpMu.RUnlock()
	if dp == nil {
		return nil
	}
	return dp.FullSync(bundle)
}

func (h *Hub) pollPeerStats() {
	stats, err := func() (map[string]tunnel.PeerStats, error) {
		h.dpMu.RLock()
		defer h.dpMu.RUnlock()
		if h.liveDP == nil {
			return nil, nil
		}
		return h.liveDP.GetStats()
	}()
	if err != nil {
		return
	}
	if stats == nil {
		return
	}
	peers, err := h.app.store.ListPeers()
	if err != nil {
		return
	}
	for _, p := range peers {
		st, ok := stats[p.PublicKey]
		if !ok {
			continue
		}
		var hs int64
		if !st.LastHandshake.IsZero() {
			hs = st.LastHandshake.Unix()
		}
		_ = h.app.store.UpdatePeerStats(p.ID, hs, st.RxBytes, st.TxBytes)
	}
	if h.statusPublisher != nil {
		h.statusPublisher.Publish()
	}
}

func repoPeerToWG(p *repo.Peer) domainruntime.WGPeer {
	return domainruntime.WGPeer{
		ID:        p.ID,
		PublicKey: p.PublicKey,
		WGIP:      p.WGIP,
		DNSName:   p.DNSName,
		GroupID:   p.GroupID,
		Enabled:   p.Enabled,
	}
}

// PeerStats is re-exported for callers that need tunnel stats shape.
type PeerStats = tunnel.PeerStats
