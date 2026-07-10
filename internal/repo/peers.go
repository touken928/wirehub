package repo

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// ListPeers returns all peers ordered by creation time (newest first).
func (s *Store) ListPeers() ([]Peer, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	var peers []Peer
	err := s.db.Order("created_at desc").Find(&peers).Error
	return peers, err
}

func (s *Store) GetPeer(id uint) (*Peer, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	var peer Peer
	if err := s.db.First(&peer, id).Error; err != nil {
		return nil, err
	}
	return &peer, nil
}

func (s *Store) CreatePeer(peer *Peer) error {
	s.lease.RLock()
	defer s.lease.RUnlock()
	return s.db.Create(peer).Error
}

// CreatePeerWithAllocatedIP atomically allocates and persists a peer address.
// The automatic peer DNS record is committed in the same transaction.
func (s *Store) CreatePeerWithAllocatedIP(peer *Peer, subnet, hubIP, dnsIP string) error {
	s.ipAllocationMu.Lock()
	defer s.ipAllocationMu.Unlock()
	s.lease.RLock()
	defer s.lease.RUnlock()
	used, err := s.collectUsedSubnetIPs(hubIP, dnsIP)
	if err != nil {
		return err
	}
	peer.WGIP, err = nextFreeHostInSubnet(subnet, used)
	if errors.Is(err, errSubnetIPUnavailable) {
		return ErrIPAllocationUnavailable
	}
	if err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(peer).Error; err != nil {
			return err
		}
		hostname := peer.DNSName
		if hostname == "" {
			hostname = peer.Name
		}
		return tx.Create(&DNSRecord{Hostname: hostname, IP: peer.WGIP, PeerID: &peer.ID}).Error
	})
}

func (s *Store) UpdatePeer(peer *Peer) error {
	s.lease.RLock()
	defer s.lease.RUnlock()
	return s.db.Save(peer).Error
}

func (s *Store) DeletePeer(id uint) error {
	s.lease.RLock()
	defer s.lease.RUnlock()
	return s.db.Delete(&Peer{}, id).Error
}

// AllocateIP picks the next free host address in the VPN subnet (hub, map VIPs, and DNS reserved).
func (s *Store) AllocateIP(subnet, hubIP, dnsIP string) (string, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	ip, err := s.allocateSubnetIP(subnet, hubIP, dnsIP)
	if errors.Is(err, errSubnetIPUnavailable) {
		return "", fmt.Errorf("no available IP in subnet")
	}
	return ip, err
}

func (s *Store) CreateDNSRecord(record *DNSRecord) error {
	s.lease.RLock()
	defer s.lease.RUnlock()
	return s.db.Create(record).Error
}

func (s *Store) DeleteDNSByPeerID(peerID uint) error {
	s.lease.RLock()
	defer s.lease.RUnlock()
	return s.db.Where("peer_id = ? AND manual = ?", peerID, false).Delete(&DNSRecord{}).Error
}

func (s *Store) UpdatePeerStats(id uint, lastHandshake, rx, tx int64) error {
	s.lease.RLock()
	defer s.lease.RUnlock()
	return s.db.Model(&Peer{}).Where("id = ?", id).Updates(map[string]interface{}{
		"last_handshake": lastHandshake,
		"rx_bytes":       rx,
		"tx_bytes":       tx,
	}).Error
}
