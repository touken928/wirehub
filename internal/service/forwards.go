package service

import (
	"errors"
	"fmt"

	"github.com/touken928/wirehub/internal/domain/forward"
	domainhub "github.com/touken928/wirehub/internal/domain/hub"
	"github.com/touken928/wirehub/internal/repo"
)

// PortForwardInput is the service contract for creating or updating a forward.
type PortForwardInput struct {
	Name       string
	ListenPort int
	Protocol   string
	TargetHost string
	TargetPort int
}

// PortForwardView is the service response shape for a forward.
type PortForwardView struct {
	ID            uint
	Name          string
	ListenPort    int
	Protocol      string
	TargetHost    string
	TargetPort    int
	TargetDisplay string
}

// ForwardList bundles port-forward rules with hub addressing hints.
type ForwardList struct {
	Rules   []PortForwardView
	HubIP   string
	HubPort int
}

var (
	ErrPortForwardConflict       = errors.New("listen port and protocol already in use")
	ErrPortForwardNotFound       = errors.New("forward not found")
	ErrPortForwardListenPortUsed = errors.New("port forward listen port is already in use")
)

type PortForwardListenPortError struct {
	Port int
}

func (e *PortForwardListenPortError) Error() string {
	return fmt.Sprintf("listen port %d is already in use", e.Port)
}

func (e *PortForwardListenPortError) Is(target error) bool {
	return target == ErrPortForwardListenPortUsed
}

// HubTunnelWebPort is the tunnel Web listen port on the hub VPN IP.
func HubTunnelWebPort() int {
	return domainhub.HubTunnelWebPort
}

// ListPortForwards returns all forward rules and hub IP for the UI.
func (a *App) ListPortForwards() (ForwardList, error) {
	rules, err := a.store.ListPortForwards()
	if err != nil {
		return ForwardList{}, err
	}
	settings, _ := a.store.GetSettings()
	hubIP := ""
	if settings != nil {
		hubIP = settings.HubIP
	}
	views := make([]PortForwardView, 0, len(rules))
	for i := range rules {
		views = append(views, portForwardView(rules[i]))
	}
	return ForwardList{Rules: views, HubIP: hubIP, HubPort: HubTunnelWebPort()}, nil
}

// CreatePortForward adds a forward rule and syncs the dataplane.
func (a *App) CreatePortForward(in PortForwardInput) (PortForwardView, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	rule, err := a.store.CreatePortForward(HubTunnelWebPort(), repoPortForwardInput(in))
	if err != nil {
		return PortForwardView{}, mapPortForwardRepoError(err)
	}
	if err := a.reconcileRuntime("port-forward creation", false); err != nil {
		return PortForwardView{}, err
	}
	return portForwardView(*rule), nil
}

// UpdatePortForward updates a forward rule and syncs the dataplane.
func (a *App) UpdatePortForward(id uint, in PortForwardInput) (PortForwardView, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	rule, err := a.store.UpdatePortForward(id, HubTunnelWebPort(), repoPortForwardInput(in))
	if err != nil {
		return PortForwardView{}, mapPortForwardRepoError(err)
	}
	if err := a.reconcileRuntime("port-forward update", false); err != nil {
		return PortForwardView{}, err
	}
	return portForwardView(*rule), nil
}

// DeletePortForward removes a forward rule and syncs the dataplane.
func (a *App) DeletePortForward(id uint) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	if err := a.store.DeletePortForward(id); err != nil {
		return mapPortForwardRepoError(err)
	}
	if err := a.reconcileRuntime("port-forward deletion", false); err != nil {
		return err
	}
	return nil
}

// ForwardErrKind classifies port-forward errors for HTTP mapping.
type ForwardErrKind int

const (
	ForwardErrOther ForwardErrKind = iota
	ForwardErrConflict
	ForwardErrNotFound
)

// ClassifyForwardErr maps store errors to HTTP-friendly kinds.
func ClassifyForwardErr(err error) ForwardErrKind {
	if errors.Is(err, ErrPortForwardConflict) {
		return ForwardErrConflict
	}
	if errors.Is(err, ErrPortForwardNotFound) {
		return ForwardErrNotFound
	}
	return ForwardErrOther
}

func repoPortForwardInput(in PortForwardInput) repo.PortForwardInput {
	return repo.PortForwardInput{Name: in.Name, ListenPort: in.ListenPort, Protocol: in.Protocol, TargetHost: in.TargetHost, TargetPort: in.TargetPort}
}

func portForwardView(rule repo.PortForward) PortForwardView {
	return PortForwardView{
		ID: rule.ID, Name: rule.Name, ListenPort: rule.ListenPort, Protocol: rule.Protocol,
		TargetHost: rule.TargetHost, TargetPort: rule.TargetPort,
		TargetDisplay: forward.ForwardDisplayTarget(rule.TargetHost, rule.TargetPort),
	}
}

func mapPortForwardRepoError(err error) error {
	if errors.Is(err, repo.ErrPortForwardConflict) {
		return ErrPortForwardConflict
	}
	if errors.Is(err, repo.ErrPortForwardNotFound) {
		return ErrPortForwardNotFound
	}
	var listenPortErr *repo.PortForwardListenPortError
	if errors.As(err, &listenPortErr) {
		return &PortForwardListenPortError{Port: listenPortErr.Port}
	}
	return err
}
