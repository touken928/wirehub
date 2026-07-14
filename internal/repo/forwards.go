package repo

import (
	"errors"
	"fmt"
	"strings"

	"github.com/touken928/wirehub/internal/domain/forward"
	"gorm.io/gorm"
)

var ErrPortForwardConflict = errors.New("listen port and protocol already in use")
var ErrPortForwardNotFound = errors.New("port forward not found")
var ErrPortForwardListenPortUsed = errors.New("port forward listen port is already in use")

type PortForwardListenPortError struct {
	Port int
}

func (e *PortForwardListenPortError) Error() string {
	return fmt.Sprintf("listen port %d is already in use", e.Port)
}

func (e *PortForwardListenPortError) Is(target error) bool {
	return target == ErrPortForwardListenPortUsed
}

type PortForwardInput struct {
	Name       string
	ListenPort int
	Protocol   string
	TargetHost string
	TargetPort int
}

func (s *Store) ListPortForwards() ([]PortForward, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	var rules []PortForward
	err := s.db.Order("listen_port asc, protocol asc").Find(&rules).Error
	return rules, err
}

func (s *Store) GetPortForward(id uint) (*PortForward, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	rule, err := getPortForwardDB(s.db, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPortForwardNotFound
	}
	return rule, err
}

func getPortForwardDB(db *gorm.DB, id uint) (*PortForward, error) {
	var rule PortForward
	if err := db.First(&rule, id).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func (s *Store) CreatePortForward(hubTunnelWebPort int, in PortForwardInput) (*PortForward, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	rule, err := normalizePortForward(in, hubTunnelWebPort)
	if err != nil {
		return nil, err
	}
	if taken, err := s.isHubListenPortUsed(rule.ListenPort, 0); err != nil {
		return nil, err
	} else if taken {
		return nil, &PortForwardListenPortError{Port: rule.ListenPort}
	}
	if err := s.db.Create(rule).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") {
			return nil, ErrPortForwardConflict
		}
		return nil, err
	}
	return rule, nil
}

func (s *Store) UpdatePortForward(id uint, hubTunnelWebPort int, in PortForwardInput) (*PortForward, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	rule, err := getPortForwardDB(s.db, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPortForwardNotFound
		}
		return nil, err
	}
	updated, err := normalizePortForward(in, hubTunnelWebPort)
	if err != nil {
		return nil, err
	}
	if updated.ListenPort != rule.ListenPort {
		if taken, err := s.isHubListenPortUsed(updated.ListenPort, rule.ID); err != nil {
			return nil, err
		} else if taken {
			return nil, &PortForwardListenPortError{Port: updated.ListenPort}
		}
	}
	updated.ID = rule.ID
	if err := s.db.Save(updated).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") {
			return nil, ErrPortForwardConflict
		}
		return nil, err
	}
	return updated, nil
}

func (s *Store) DeletePortForward(id uint) error {
	s.lease.RLock()
	defer s.lease.RUnlock()
	result := s.db.Delete(&PortForward{}, id)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func normalizePortForward(in PortForwardInput, hubTunnelWebPort int) (*PortForward, error) {
	proto, err := forward.ValidateForwardProtocol(in.Protocol)
	if err != nil {
		return nil, err
	}
	if err := forward.ValidateForwardListenPort(in.ListenPort, hubTunnelWebPort, proto); err != nil {
		return nil, err
	}
	if err := forward.ValidateForwardPort(in.TargetPort, "target port"); err != nil {
		return nil, err
	}
	targetHost, err := forward.ValidateForwardTargetHost(in.TargetHost)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if len(name) > 64 {
		return nil, fmt.Errorf("name must be at most 64 characters")
	}
	return &PortForward{
		Name:       name,
		ListenPort: in.ListenPort,
		Protocol:   proto,
		TargetHost: targetHost,
		TargetPort: in.TargetPort,
	}, nil
}
