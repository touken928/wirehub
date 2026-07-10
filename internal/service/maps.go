package service

import (
	"errors"

	"github.com/touken928/wirehub/internal/repo"
)

var ErrAllowedGroupNotFound = errors.New("allowed group not found")

var ErrMapIPUnavailable = repo.ErrMapIPUnavailable

// ListMapDetails returns all service maps with allowed groups.
func (a *App) ListMapDetails() ([]repo.MapDetail, error) {
	return a.store.ListMapDetails()
}

// CreateServiceMap adds a map after validating allowed groups exist.
func (a *App) CreateServiceMap(in repo.MapInput) (*repo.MapDetail, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.createServiceMap(in)
}

func (a *App) createServiceMap(in repo.MapInput) (*repo.MapDetail, error) {
	for _, gid := range in.AllowedGroups {
		if _, err := a.store.GetGroup(gid); err != nil {
			return nil, ErrAllowedGroupNotFound
		}
	}
	detail, err := a.store.CreateServiceMap(in)
	if err != nil {
		return nil, err
	}
	if err := a.reconcileRuntime("map creation", false); err != nil {
		return nil, err
	}
	return detail, nil
}

// UpdateServiceMap updates a map and syncs runtime state.
func (a *App) UpdateServiceMap(id uint, in repo.MapInput) (*repo.MapDetail, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.updateServiceMap(id, in)
}

func (a *App) updateServiceMap(id uint, in repo.MapInput) (*repo.MapDetail, error) {
	for _, gid := range in.AllowedGroups {
		if _, err := a.store.GetGroup(gid); err != nil {
			return nil, ErrAllowedGroupNotFound
		}
	}
	detail, err := a.store.UpdateServiceMap(id, in)
	if err != nil {
		return nil, err
	}
	if err := a.reconcileRuntime("map update", false); err != nil {
		return nil, err
	}
	return detail, nil
}

// DeleteServiceMap removes a map and syncs runtime state.
func (a *App) DeleteServiceMap(id uint) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	if err := a.store.DeleteServiceMap(id); err != nil {
		return err
	}
	if err := a.reconcileRuntime("map deletion", false); err != nil {
		return err
	}
	return nil
}

// ClassifyMapErr reports whether an error is a slug conflict.
func ClassifyMapErr(err error) bool {
	return errors.Is(err, repo.ErrMapSlugConflict)
}
