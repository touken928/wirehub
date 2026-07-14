package service

import (
	"errors"

	mapdom "github.com/touken928/wirehub/internal/domain/map"
	"github.com/touken928/wirehub/internal/repo"
)

// MapInput is the service contract for creating or updating a map.
type MapInput struct {
	Name            string
	Slug            string
	TargetHost      string
	AllowedGroupIDs []uint
}

// MapView is the public service representation of a map.
type MapView struct {
	ID              uint
	Name            string
	Slug            string
	TargetHost      string
	VirtualIP       string
	TargetDisplay   string
	AllowedGroupIDs []uint
	FQDN            string
}

var (
	ErrAllowedGroupNotFound = errors.New("allowed group not found")
	ErrMapSlugConflict      = errors.New("map slug already in use")
	ErrMapIPUnavailable     = errors.New("no map virtual ip available in subnet")
)

// ListMapDetails returns all service maps with allowed groups.
func (a *App) ListMapDetails() ([]MapView, error) {
	details, err := a.store.ListMapDetails()
	if err != nil {
		return nil, err
	}
	views := make([]MapView, 0, len(details))
	for _, detail := range details {
		views = append(views, mapView(detail))
	}
	return views, nil
}

// CreateServiceMap adds a map after validating allowed groups exist.
func (a *App) CreateServiceMap(in MapInput) (*MapView, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.createServiceMap(in)
}

func (a *App) createServiceMap(in MapInput) (*MapView, error) {
	for _, gid := range in.AllowedGroupIDs {
		if _, err := a.store.GetGroup(gid); err != nil {
			return nil, ErrAllowedGroupNotFound
		}
	}
	detail, err := a.store.CreateServiceMap(repo.MapInput{Name: in.Name, Slug: in.Slug, TargetHost: in.TargetHost, AllowedGroups: in.AllowedGroupIDs})
	if err != nil {
		return nil, mapRepoError(err)
	}
	if err := a.reconcileRuntime("map creation", false); err != nil {
		return nil, err
	}
	view := mapView(*detail)
	return &view, nil
}

// UpdateServiceMap updates a map and syncs runtime state.
func (a *App) UpdateServiceMap(id uint, in MapInput) (*MapView, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.updateServiceMap(id, in)
}

func (a *App) updateServiceMap(id uint, in MapInput) (*MapView, error) {
	for _, gid := range in.AllowedGroupIDs {
		if _, err := a.store.GetGroup(gid); err != nil {
			return nil, ErrAllowedGroupNotFound
		}
	}
	detail, err := a.store.UpdateServiceMap(id, repo.MapInput{Name: in.Name, Slug: in.Slug, TargetHost: in.TargetHost, AllowedGroups: in.AllowedGroupIDs})
	if err != nil {
		return nil, mapRepoError(err)
	}
	if err := a.reconcileRuntime("map update", false); err != nil {
		return nil, err
	}
	view := mapView(*detail)
	return &view, nil
}

// DeleteServiceMap removes a map and syncs runtime state.
func (a *App) DeleteServiceMap(id uint) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	if err := a.store.DeleteServiceMap(id); err != nil {
		return mapRepoError(err)
	}
	if err := a.reconcileRuntime("map deletion", false); err != nil {
		return err
	}
	return nil
}

func mapView(detail repo.MapDetail) MapView {
	return MapView{
		ID: detail.ID, Name: detail.Name, Slug: detail.Slug, TargetHost: detail.TargetHost,
		VirtualIP: detail.VirtualIP, TargetDisplay: detail.TargetDisplay,
		AllowedGroupIDs: detail.AllowedGroups, FQDN: mapdom.MapFQDN(detail.Slug),
	}
}

func mapRepoError(err error) error {
	if errors.Is(err, repo.ErrMapSlugConflict) {
		return ErrMapSlugConflict
	}
	if errors.Is(err, repo.ErrMapIPUnavailable) {
		return ErrMapIPUnavailable
	}
	return err
}
