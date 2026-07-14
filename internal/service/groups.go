package service

import (
	"errors"
	"sort"
	"time"

	domainpeer "github.com/touken928/wirehub/internal/domain/peer"
	"github.com/touken928/wirehub/internal/repo"
)

// GroupView is a peer group with member count.
type GroupView struct {
	ID              uint
	Name            string
	PosX            float64
	PosY            float64
	AllowIntraGroup bool
	MemberCount     int64
}

type CreateGroupInput struct {
	Name string
	PosX float64
	PosY float64
}

type UpdateGroupInput struct {
	Name            *string
	PosX            *float64
	PosY            *float64
	AllowIntraGroup *bool
}

type GroupMutationView struct {
	ID              uint
	Name            string
	PosX            float64
	PosY            float64
	AllowIntraGroup bool
}

type GroupLinkInput struct {
	FromGroupID   uint
	ToGroupID     uint
	Bidirectional bool
}

// GroupLinkView is a graph edge between peer groups.
type GroupLinkView struct {
	ID            uint
	FromGroupID   uint
	ToGroupID     uint
	Bidirectional bool
}

// GroupPeerView is the public peer data displayed in the group graph.
type GroupPeerView struct {
	ID            uint
	Name          string
	PublicKey     string
	WGIP          string
	GroupID       uint
	Enabled       bool
	DNSName       string
	LastHandshake int64
	RxBytes       int64
	TxBytes       int64
	CreatedAt     time.Time
	FQDN          string
}

// GroupGraphGroupView is a graph group with its newest peers.
type GroupGraphGroupView struct {
	ID              uint
	Name            string
	PosX            float64
	PosY            float64
	AllowIntraGroup bool
	MemberCount     int64
	Peers           []GroupPeerView
}

// GroupGraphView is the complete group topology payload.
type GroupGraphView struct {
	Groups []GroupGraphGroupView
	Links  []GroupLinkView
}

// ListGroups returns all groups with member counts.
func (a *App) ListGroups() ([]GroupView, error) {
	groups, err := a.store.ListGroups()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	counts, err := a.store.CountPeersByGroup()
	if err != nil {
		return nil, err
	}
	out := make([]GroupView, 0, len(groups))
	for _, g := range groups {
		out = append(out, GroupView{ID: g.ID, Name: g.Name, PosX: g.PosX, PosY: g.PosY, AllowIntraGroup: g.AllowIntraGroup, MemberCount: counts[g.ID]})
	}
	return out, nil
}

// GetGroupNameMap returns all group IDs to their display name.
func (a *App) getGroupNameMap() map[uint]string {
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
func (a *App) CreateGroup(in CreateGroupInput) (GroupMutationView, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	g, err := a.store.CreateGroup(in.Name, in.PosX, in.PosY)
	if err != nil {
		return GroupMutationView{}, err
	}
	if err := a.reconcileRuntime("group creation", false); err != nil {
		return GroupMutationView{}, err
	}
	return groupMutationView(*g), nil
}

// UpdateGroup applies partial group changes. Name-only changes do not affect runtime state.
func (a *App) UpdateGroup(id uint, in UpdateGroupInput) (GroupMutationView, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	g, err := a.store.GetGroup(id)
	if err != nil {
		return GroupMutationView{}, err
	}
	if in.Name != nil {
		g, err = a.store.RenameGroup(id, *in.Name)
		if err != nil {
			return GroupMutationView{}, err
		}
	}
	needsRuntime := in.PosX != nil || in.PosY != nil || in.AllowIntraGroup != nil
	if in.PosX != nil {
		g.PosX = *in.PosX
	}
	if in.PosY != nil {
		g.PosY = *in.PosY
	}
	if in.AllowIntraGroup != nil {
		g.AllowIntraGroup = *in.AllowIntraGroup
	}
	if needsRuntime {
		if err := a.store.UpdateGroup(g); err != nil {
			return GroupMutationView{}, err
		}
		if err := a.reconcileRuntime("group update", false); err != nil {
			return groupMutationView(*g), err
		}
	}
	return groupMutationView(*g), nil
}

func groupMutationView(g repo.PeerGroup) GroupMutationView {
	return GroupMutationView{ID: g.ID, Name: g.Name, PosX: g.PosX, PosY: g.PosY, AllowIntraGroup: g.AllowIntraGroup}
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

// GroupGraph returns data for the groups canvas.
func (a *App) GroupGraph() (GroupGraphView, error) {
	groups, err := a.store.ListGroups()
	if err != nil {
		return GroupGraphView{}, err
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	links, err := a.store.ListGroupLinks()
	if err != nil {
		return GroupGraphView{}, err
	}
	peers, err := a.store.ListPeers()
	if err != nil {
		return GroupGraphView{}, err
	}
	sort.SliceStable(peers, func(i, j int) bool { return peers[i].CreatedAt.After(peers[j].CreatedAt) })
	groupPeers := make(map[uint][]GroupPeerView, len(groups))
	for _, p := range peers {
		groupPeers[p.GroupID] = append(groupPeers[p.GroupID], GroupPeerView{
			ID: p.ID, Name: p.Name, PublicKey: p.PublicKey, WGIP: p.WGIP, GroupID: p.GroupID,
			Enabled: p.Enabled, DNSName: p.DNSName, LastHandshake: p.LastHandshake,
			RxBytes: p.RxBytes, TxBytes: p.TxBytes, CreatedAt: p.CreatedAt,
			FQDN: domainpeer.PeerFQDN(p.Name),
		})
	}
	groupViews := make([]GroupGraphGroupView, 0, len(groups))
	for _, g := range groups {
		groupViews = append(groupViews, GroupGraphGroupView{
			ID: g.ID, Name: g.Name, PosX: g.PosX, PosY: g.PosY, AllowIntraGroup: g.AllowIntraGroup,
			MemberCount: int64(len(groupPeers[g.ID])), Peers: groupPeers[g.ID],
		})
	}
	linkViews := make([]GroupLinkView, 0, len(links))
	for _, l := range links {
		linkViews = append(linkViews, GroupLinkView{ID: l.ID, FromGroupID: l.FromGroupID, ToGroupID: l.ToGroupID, Bidirectional: l.Bidirectional})
	}
	return GroupGraphView{Groups: groupViews, Links: linkViews}, nil
}

// CreateGroupLink adds or replaces a directed group link.
func (a *App) CreateGroupLink(in GroupLinkInput) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	if _, err := a.store.GetGroup(in.FromGroupID); err != nil {
		return err
	}
	if _, err := a.store.GetGroup(in.ToGroupID); err != nil {
		return err
	}
	if in.FromGroupID == in.ToGroupID {
		return ErrSelfLink
	}
	if err := a.store.UpsertGroupLink(in.FromGroupID, in.ToGroupID, in.Bidirectional); err != nil {
		return err
	}
	if err := a.reconcileRuntime("group-link creation", false); err != nil {
		return err
	}
	return nil
}

// DeleteGroupLink removes a directed group link.
func (a *App) DeleteGroupLink(in GroupLinkInput) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	if err := a.store.DeleteGroupLink(in.FromGroupID, in.ToGroupID); err != nil {
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
// ErrSelfLink is returned when linking a group to itself.
var ErrSelfLink = errors.New("cannot link a group to itself")
