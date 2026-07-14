package service

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/touken928/wirehub/internal/config"
	dompolicy "github.com/touken928/wirehub/internal/domain/policy"
	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
	"github.com/touken928/wirehub/internal/repo"
)

func testApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	st, err := repo.New(&config.RuntimeConfig{DatabasePath: filepath.Join(dir, "wirehub.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Setup(repo.SetupInput{
		Endpoint: "example.com", Subnet: "100.127.0.0/24",
		AdminUsername: "admin", AdminPassword: "testpass123",
		ListenPort: 8443, MTU: 1420, StatusInterval: 1,
		ServerPrivateKey: "private", ServerPublicKey: "public",
	}); err != nil {
		t.Fatal(err)
	}
	return NewApp(st, testKeyGenerator)
}

func TestCreateGroupLink_ReplacesDirection(t *testing.T) {
	a := testApp(t)

	g1, err := a.CreateGroup(CreateGroupInput{Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	g2, err := a.CreateGroup(CreateGroupInput{Name: "b", PosX: 100})
	if err != nil {
		t.Fatal(err)
	}

	// Create bidirectional link
	if err := a.CreateGroupLink(GroupLinkInput{FromGroupID: g1.ID, ToGroupID: g2.ID, Bidirectional: true}); err != nil {
		t.Fatal(err)
	}

	links, err := a.store.ListGroupLinks()
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || !links[0].Bidirectional {
		t.Fatal("expected one bidirectional link")
	}

	// Replace with unidirectional link (same pair, different direction)
	// The old code had a no-op bug here; test that replacement works
	if err := a.CreateGroupLink(GroupLinkInput{FromGroupID: g2.ID, ToGroupID: g1.ID}); err != nil {
		t.Fatal(err)
	}

	links, err = a.store.ListGroupLinks()
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link after replacement, got %d", len(links))
	}
	if links[0].Bidirectional {
		t.Fatal("expected unidirectional after replacement")
	}
	// The unidirectional link should keep from→to order as given (g2 → g1)
	if links[0].FromGroupID != g2.ID || links[0].ToGroupID != g1.ID {
		t.Fatalf("expected link from %d→%d, got %+v", g2.ID, g1.ID, links[0])
	}
}

func TestCreateGroupLink_ReplacesToBidirectional(t *testing.T) {
	a := testApp(t)

	g1, err := a.CreateGroup(CreateGroupInput{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	g2, err := a.CreateGroup(CreateGroupInput{Name: "y", PosX: 100})
	if err != nil {
		t.Fatal(err)
	}

	// Create unidirectional link
	if err := a.CreateGroupLink(GroupLinkInput{FromGroupID: g1.ID, ToGroupID: g2.ID}); err != nil {
		t.Fatal(err)
	}

	// Replace with bidirectional
	if err := a.CreateGroupLink(GroupLinkInput{FromGroupID: g1.ID, ToGroupID: g2.ID, Bidirectional: true}); err != nil {
		t.Fatal(err)
	}

	links, err := a.store.ListGroupLinks()
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if !links[0].Bidirectional {
		t.Fatal("expected bidirectional link after replacement")
	}
}

func TestDeleteGroup_ClearsMapAllow(t *testing.T) {
	a := testApp(t)

	g, err := a.CreateGroup(CreateGroupInput{Name: "admins"})
	if err != nil {
		t.Fatal(err)
	}
	g2, err := a.CreateGroup(CreateGroupInput{Name: "users", PosX: 100})
	if err != nil {
		t.Fatal(err)
	}

	detail, err := a.CreateServiceMap(MapInput{
		Slug: "admin-svc", TargetHost: "10.0.0.1",
		AllowedGroupIDs: []uint{g.ID, g2.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify 2 allow rows
	groups, err := a.store.ListMapGroupIDs(detail.ID)
	if err != nil || len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %v err=%v", groups, err)
	}

	// Delete g2
	if err := a.DeleteGroup(g2.ID); err != nil {
		t.Fatal(err)
	}

	// Verify only g's allow row remains
	groups, err = a.store.ListMapGroupIDs(detail.ID)
	if err != nil || len(groups) != 1 || groups[0] != g.ID {
		t.Fatalf("expected 1 group (g), got %v err=%v", groups, err)
	}
}

func TestCreateGroupLink_SelfLinkError(t *testing.T) {
	a := testApp(t)
	g, err := a.CreateGroup(CreateGroupInput{Name: "self"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CreateGroupLink(GroupLinkInput{FromGroupID: g.ID, ToGroupID: g.ID}); err != ErrSelfLink {
		t.Fatalf("expected ErrSelfLink, got %v", err)
	}
}

func TestCreateGroupLink_NonexistentGroup(t *testing.T) {
	a := testApp(t)
	g, err := a.CreateGroup(CreateGroupInput{Name: "exists"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CreateGroupLink(GroupLinkInput{FromGroupID: g.ID, ToGroupID: 9999}); err == nil {
		t.Fatal("expected error for nonexistent group")
	}
}

func TestUpdateGroup_NameOnlyDoesNotReconcile(t *testing.T) {
	a := testApp(t)
	g, err := a.CreateGroup(CreateGroupInput{Name: "before"})
	if err != nil {
		t.Fatal(err)
	}
	net := &phase5Network{}
	a.Hub.SetNetworkRuntime(net)
	name := "after"
	view, err := a.UpdateGroup(g.ID, UpdateGroupInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if view.Name != name || net.stopCount != 0 || len(net.starts) != 0 {
		t.Fatalf("view=%+v stop=%d starts=%d", view, net.stopCount, len(net.starts))
	}
}

func TestUpdateGroup_RuntimeFailurePersistsMutation(t *testing.T) {
	a := testApp(t)
	g, err := a.CreateGroup(CreateGroupInput{Name: "before"})
	if err != nil {
		t.Fatal(err)
	}
	net := &phase5Network{startErr: errors.New("restart failed")}
	a.Hub.SetNetworkRuntime(net)
	installPhase5Dataplane(a, errors.New("group sync failed"))
	pos := 42.0
	_, err = a.UpdateGroup(g.ID, UpdateGroupInput{PosX: &pos})
	if err == nil || !IsRuntimeFailure(err) {
		t.Fatalf("update error = %v, want runtime failure", err)
	}
	stored, err := a.store.GetGroup(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PosX != pos {
		t.Fatalf("persisted pos_x = %v, want %v", stored.PosX, pos)
	}
}

type countingGroupDataplane struct {
	fullSyncs int
}

func (d *countingGroupDataplane) ApplyPolicy(dompolicy.AccessPolicySpec) error { return nil }
func (d *countingGroupDataplane) UpdateDNS(domainruntime.DNSCatalog, []domainruntime.WGPeer) error {
	return nil
}
func (d *countingGroupDataplane) FullSync(domainruntime.SyncBundle) error {
	d.fullSyncs++
	return nil
}
func (d *countingGroupDataplane) GetStats() (map[string]domainruntime.PeerStats, error) {
	return nil, nil
}

func TestGroupDeletesAreIdempotentAndReconcileAbsentObjects(t *testing.T) {
	a := testApp(t)
	net := &phase5Network{}
	a.Hub.SetNetworkRuntime(net)
	dp := &countingGroupDataplane{}
	a.OnStarted(dp)
	if err := a.DeleteGroup(9999); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteGroup(9999); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteGroupLink(GroupLinkInput{FromGroupID: 9998, ToGroupID: 9997}); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteGroupLink(GroupLinkInput{FromGroupID: 9998, ToGroupID: 9997}); err != nil {
		t.Fatal(err)
	}
	if dp.fullSyncs != 4 {
		t.Fatalf("reconciliation count = %d, want 4", dp.fullSyncs)
	}
}
