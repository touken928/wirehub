package service

import (
	"errors"

	"github.com/touken928/wirehub/internal/repo"
)

// GroupView is a peer group with member count.
type GroupView struct {
	repo.PeerGroup
	MemberCount int64
}

// ListGroups returns all groups with member counts.
func (a *App) ListGroups() ([]GroupView, error) {
	groups, err := a.store.ListGroups()
	if err != nil {
		return nil, err
	}
	counts, err := a.store.CountPeersByGroup()
	if err != nil {
		return nil, err
	}
	out := make([]GroupView, 0, len(groups))
	for _, g := range groups {
		out = append(out, GroupView{PeerGroup: g, MemberCount: counts[g.ID]})
	}
	return out, nil
}

// GetGroupNameMap returns all group IDs to their display name.
func (a *App) GetGroupNameMap() map[uint]string {
	groups, err := a.store.ListGroups()
	if err != nil {
		return nil
	}
	out := make(map[uint]string, len(groups))
	for _, g := range groups {
		out[g.ID] = g.Name
	}
	return out
}

// CreateGroup adds a new peer group.
func (a *App) CreateGroup(name string, posX, posY float64) (*repo.PeerGroup, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	g, err := a.store.CreateGroup(name, posX, posY)
	if err != nil {
		return nil, err
	}
	if err := a.reconcileRuntime("group creation", false); err != nil {
		return nil, err
	}
	return g, nil
}

// GetGroup loads a group by id.
func (a *App) GetGroup(id uint) (*repo.PeerGroup, error) {
	return a.store.GetGroup(id)
}

// UpdateGroup persists group fields.
func (a *App) UpdateGroup(g *repo.PeerGroup) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	if err := a.store.UpdateGroup(g); err != nil {
		return err
	}
	if err := a.reconcileRuntime("group update", false); err != nil {
		return err
	}
	return nil
}

// RenameGroup changes a group's display name.
func (a *App) RenameGroup(id uint, name string) (*repo.PeerGroup, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	g, err := a.store.RenameGroup(id, name)
	if err != nil {
		return nil, err
	}
	if err := a.reconcileRuntime("group rename", false); err != nil {
		return nil, err
	}
	return g, nil
}

// DeleteGroup removes a group and refreshes ACL rules.
func (a *App) DeleteGroup(id uint) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	if err := a.store.DeleteGroup(id); err != nil {
		return err
	}
	if err := a.reconcileRuntime("group deletion", false); err != nil {
		return err
	}
	return nil
}

// GroupGraphData holds groups, links, and peers for the topology UI.
type GroupGraphData struct {
	Groups []repo.PeerGroup
	Links  []repo.GroupLink
	Peers  []repo.Peer
}

// GroupGraph returns data for the groups canvas.
func (a *App) GroupGraph() (GroupGraphData, error) {
	groups, err := a.store.ListGroups()
	if err != nil {
		return GroupGraphData{}, err
	}
	links, err := a.store.ListGroupLinks()
	if err != nil {
		return GroupGraphData{}, err
	}
	peers, err := a.store.ListPeers()
	if err != nil {
		return GroupGraphData{}, err
	}
	return GroupGraphData{Groups: groups, Links: links, Peers: peers}, nil
}

// CreateGroupLink adds or replaces a directed group link.
func (a *App) CreateGroupLink(fromID, toID uint, bidirectional bool) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	if _, err := a.store.GetGroup(fromID); err != nil {
		return err
	}
	if _, err := a.store.GetGroup(toID); err != nil {
		return err
	}
	if fromID == toID {
		return ErrSelfLink
	}
	if err := a.store.UpsertGroupLink(fromID, toID, bidirectional); err != nil {
		return err
	}
	if err := a.reconcileRuntime("group-link creation", false); err != nil {
		return err
	}
	return nil
}

// DeleteGroupLink removes a directed group link.
func (a *App) DeleteGroupLink(fromID, toID uint) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	if err := a.store.DeleteGroupLink(fromID, toID); err != nil {
		return err
	}
	if err := a.reconcileRuntime("group-link deletion", false); err != nil {
		return err
	}
	return nil
}

// UpdateGroupLayout saves canvas positions for groups.
func (a *App) UpdateGroupLayout(items []GroupLayoutItem) error {
	for _, item := range items {
		g, err := a.store.GetGroup(item.ID)
		if err != nil {
			continue
		}
		g.PosX = item.PosX
		g.PosY = item.PosY
		_ = a.store.UpdateGroup(g)
	}
	return nil
}

// GroupLayoutItem is one node position on the groups graph.
type GroupLayoutItem struct {
	ID   uint
	PosX float64
	PosY float64
}

// UpdateGroupFields applies name, position, and intra-group policy changes.
func (a *App) UpdateGroupFields(id uint, name *string, posX, posY *float64, allowIntra *bool) (*repo.PeerGroup, bool, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	g, err := a.store.GetGroup(id)
	if err != nil {
		return nil, false, err
	}
	if name != nil {
		g, err = a.store.RenameGroup(id, *name)
		if err != nil {
			return nil, false, err
		}
	}
	if posX != nil {
		g.PosX = *posX
	}
	if posY != nil {
		g.PosY = *posY
	}
	if allowIntra != nil {
		g.AllowIntraGroup = *allowIntra
	}
	needsSave := posX != nil || posY != nil || allowIntra != nil
	if needsSave {
		if err := a.store.UpdateGroup(g); err != nil {
			return nil, false, err
		}
		if err := a.reconcileRuntime("group update", false); err != nil {
			return g, true, err
		}
	}
	return g, needsSave, nil
}

// ErrSelfLink is returned when linking a group to itself.
var ErrSelfLink = errors.New("cannot link a group to itself")
