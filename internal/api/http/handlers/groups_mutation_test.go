package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	_ "github.com/glebarez/sqlite"
	dompolicy "github.com/touken928/wirehub/internal/domain/policy"
	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
	"github.com/touken928/wirehub/internal/repo"
	"github.com/touken928/wirehub/internal/service"
)

func setupForwardHandlerWithStore(t *testing.T) (*Server, *repo.Store, *service.App) {
	t.Helper()
	srv, store, _ := newTestServer(t)
	if err := store.Setup(testSetupInput()); err != nil {
		t.Fatal(err)
	}
	return srv, store, srv.App
}

func TestGroupMutationHTTPContracts(t *testing.T) {
	srv, _ := setupForwardHandler(t)

	create, w := forwardContext(http.MethodPost, "/api/groups", `{"name":"alpha","pos_x":1,"pos_y":2}`)
	CreateGroup(srv, create)
	if w.Code != http.StatusCreated || w.Body.String() != `{"id":2,"name":"alpha","pos_x":1,"pos_y":2,"allow_intra_group":true,"MemberCount":0}` {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}

	update, w := forwardContext(http.MethodPut, "/api/groups/2", `{"name":"renamed"}`)
	update.Params = mapID(2)
	UpdateGroup(srv, update)
	if w.Code != http.StatusOK || w.Body.String() != `{"id":2,"name":"renamed","pos_x":1,"pos_y":2,"allow_intra_group":true,"MemberCount":0}` {
		t.Fatalf("name update = %d %s", w.Code, w.Body.String())
	}

	update, w = forwardContext(http.MethodPut, "/api/groups/2", `{"pos_x":9,"allow_intra_group":false}`)
	update.Params = mapID(2)
	UpdateGroup(srv, update)
	if w.Code != http.StatusOK || w.Body.String() != `{"id":2,"name":"renamed","pos_x":9,"pos_y":2,"allow_intra_group":false,"MemberCount":0}` {
		t.Fatalf("field update = %d %s", w.Code, w.Body.String())
	}

	link, w := forwardContext(http.MethodPost, "/api/groups/links", `{"from_group_id":1,"to_group_id":2}`)
	CreateGroupLink(srv, link)
	if w.Code != http.StatusCreated || w.Body.String() != `{"ok":true}` {
		t.Fatalf("default link = %d %s", w.Code, w.Body.String())
	}
	link, w = forwardContext(http.MethodPost, "/api/groups/links", `{"from_group_id":2,"to_group_id":1,"bidirectional":false}`)
	CreateGroupLink(srv, link)
	if w.Code != http.StatusCreated || w.Body.String() != `{"ok":true}` {
		t.Fatalf("directed link = %d %s", w.Code, w.Body.String())
	}

	removeLink, w := forwardContext(http.MethodDelete, "/api/groups/links", `{"from_group_id":99,"to_group_id":98}`)
	DeleteGroupLink(srv, removeLink)
	if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}` {
		t.Fatalf("absent link delete = %d %s", w.Code, w.Body.String())
	}
	removeLink, w = forwardContext(http.MethodDelete, "/api/groups/links", `{"from_group_id":99,"to_group_id":98}`)
	DeleteGroupLink(srv, removeLink)
	if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}` {
		t.Fatalf("idempotent link delete = %d %s", w.Code, w.Body.String())
	}

	remove, w := forwardContext(http.MethodDelete, "/api/groups/99", "")
	remove.Params = mapID(99)
	DeleteGroup(srv, remove)
	if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}` {
		t.Fatalf("absent group delete = %d %s", w.Code, w.Body.String())
	}
}

func TestGroupCreateRuntimeFailureReturns503AfterPersistence(t *testing.T) {
	srv, app := setupForwardHandler(t)
	app.Hub.SetNetworkRuntime(&forwardFailRuntime{startErr: errors.New("restart failed")})
	app.OnStarted(&forwardTestDataplane{fullSyncErr: errors.New("full sync failed")})

	create, w := forwardContext(http.MethodPost, "/api/groups", `{"name":"persisted"}`)
	CreateGroup(srv, create)
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != `{"error":"group creation persisted but runtime is stopped: full sync failed\nrestart failed"}` {
		t.Fatalf("runtime create = %d %s", w.Code, w.Body.String())
	}
	groups, err := app.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, group := range groups {
		if group.Name == "persisted" {
			found = true
		}
	}
	if !found {
		t.Fatal("runtime-failed group was not persisted")
	}
}

type groupLayoutRuntime struct {
	starts int
	stops  int
}

func (r *groupLayoutRuntime) Start(domainruntime.SyncBundle) error { r.starts++; return nil }
func (r *groupLayoutRuntime) Stop() error                          { r.stops++; return nil }
func (r *groupLayoutRuntime) SetDNSUpstream([]string)              {}

func TestGroupLayoutHTTPAlways200IgnoresErrorsAndDoesNotReconcile(t *testing.T) {
	srv, app := setupForwardHandler(t)
	runtime := &groupLayoutRuntime{}
	app.Hub.SetNetworkRuntime(runtime)
	app.OnStarted(&forwardTestDataplane{})

	layout, w := forwardContext(http.MethodPut, "/api/groups/layout", `{"groups":[{"id":999,"pos_x":3,"pos_y":4}]}`)
	UpdateGroupLayout(srv, layout)
	if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}` {
		t.Fatalf("ignored layout error = %d %s", w.Code, w.Body.String())
	}
	layout, w = forwardContext(http.MethodPut, "/api/groups/layout", `{"groups":[{"id":1,"pos_x":3,"pos_y":4}]}`)
	UpdateGroupLayout(srv, layout)
	if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}` {
		t.Fatalf("layout = %d %s", w.Code, w.Body.String())
	}
	if runtime.starts != 0 || runtime.stops != 0 {
		t.Fatalf("layout reconciled runtime: starts=%d stops=%d", runtime.starts, runtime.stops)
	}
}

type groupMutationDataplane struct {
	path   string
	query  string
	called bool
}

func (d *groupMutationDataplane) ApplyPolicy(dompolicy.AccessPolicySpec) error { return nil }
func (d *groupMutationDataplane) UpdateDNS(domainruntime.DNSCatalog, []domainruntime.WGPeer) error {
	return nil
}
func (d *groupMutationDataplane) FullSync(domainruntime.SyncBundle) error {
	if d.called {
		return nil
	}
	d.called = true
	db, err := sql.Open("sqlite", d.path)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(d.query)
	return err
}
func (d *groupMutationDataplane) GetStats() (map[string]domainruntime.PeerStats, error) {
	return nil, nil
}

func TestGroupCreateFallbackAfterRelistFails(t *testing.T) {
	srv, store, _ := newTestServer(t)
	if err := store.Setup(testSetupInput()); err != nil {
		t.Fatal(err)
	}
	app := srv.App
	app.Hub.SetNetworkRuntime(&groupLayoutRuntime{})
	app.OnStarted(&groupMutationDataplane{path: store.DatabasePath(), query: "ALTER TABLE peer_groups RENAME TO peer_groups_broken"})

	create, w := forwardContext(http.MethodPost, "/api/groups", `{"name":"alpha"}`)
	CreateGroup(srv, create)
	if w.Code != http.StatusCreated || w.Body.String() != `{"id":2,"name":"alpha"}` {
		t.Fatalf("create relist failure = %d %s", w.Code, w.Body.String())
	}
}

func TestGroupCreateFallbackWhenRelistMissesCreatedGroup(t *testing.T) {
	srv, store, _ := newTestServer(t)
	if err := store.Setup(testSetupInput()); err != nil {
		t.Fatal(err)
	}
	app := srv.App
	app.Hub.SetNetworkRuntime(&groupLayoutRuntime{})
	app.OnStarted(&groupMutationDataplane{path: store.DatabasePath(), query: "DELETE FROM peer_groups WHERE name = 'alpha'"})

	create, w := forwardContext(http.MethodPost, "/api/groups", `{"name":"alpha"}`)
	CreateGroup(srv, create)
	if w.Code != http.StatusCreated || w.Body.String() != `{"id":2,"name":"alpha"}` {
		t.Fatalf("create relist miss = %d %s", w.Code, w.Body.String())
	}
}

func TestGroupUpdateReturns500WhenRelistFails(t *testing.T) {
	srv, store, _ := newTestServer(t)
	if err := store.Setup(testSetupInput()); err != nil {
		t.Fatal(err)
	}
	app := srv.App
	g, err := app.CreateGroup(service.CreateGroupInput{Name: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	app.Hub.SetNetworkRuntime(&groupLayoutRuntime{})
	app.OnStarted(&groupMutationDataplane{path: store.DatabasePath(), query: "ALTER TABLE peer_groups RENAME TO peer_groups_broken"})
	update, w := forwardContext(http.MethodPut, "/api/groups/"+strconv.Itoa(int(g.ID)), `{"pos_x":4}`)
	update.Params = mapID(g.ID)
	UpdateGroup(srv, update)
	if w.Code != http.StatusInternalServerError || w.Body.String() != `{"error":"SQL logic error: no such table: peer_groups (1)"}` {
		t.Fatalf("update relist failure = %d %s", w.Code, w.Body.String())
	}
}

func TestGroupLinksPersistEndpointDirectionAndDefaultBidirectionality(t *testing.T) {
	srv, store, _ := newTestServer(t)
	if err := store.Setup(testSetupInput()); err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateGroup("first", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateGroup("second", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	link, w := forwardContext(http.MethodPost, "/api/groups/links", `{"from_group_id":`+strconv.Itoa(int(first.ID))+`,"to_group_id":`+strconv.Itoa(int(second.ID))+`}`)
	CreateGroupLink(srv, link)
	if w.Code != http.StatusCreated || w.Body.String() != `{"ok":true}` {
		t.Fatalf("default link = %d %s", w.Code, w.Body.String())
	}
	links, err := store.ListGroupLinks()
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].FromGroupID != first.ID || links[0].ToGroupID != second.ID || !links[0].Bidirectional {
		t.Fatalf("persisted default link = %+v", links)
	}
	link, w = forwardContext(http.MethodPost, "/api/groups/links", `{"from_group_id":`+strconv.Itoa(int(second.ID))+`,"to_group_id":`+strconv.Itoa(int(first.ID))+`,"bidirectional":false}`)
	CreateGroupLink(srv, link)
	links, err = store.ListGroupLinks()
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].FromGroupID != second.ID || links[0].ToGroupID != first.ID || links[0].Bidirectional {
		t.Fatalf("persisted directed link = %+v", links)
	}
}

func TestGroupDeleteAndLinkDeleteRuntimeFailuresPersist(t *testing.T) {
	srv, store, _ := setupForwardHandlerWithStore(t)
	app := srv.App
	group, err := app.CreateGroup(service.CreateGroupInput{Name: "gone"})
	if err != nil {
		t.Fatal(err)
	}
	remove, w := forwardContext(http.MethodDelete, "/api/groups/"+strconv.Itoa(int(group.ID)), "")
	remove.Params = mapID(group.ID)
	app.Hub.SetNetworkRuntime(&forwardFailRuntime{startErr: errors.New("restart failed")})
	app.OnStarted(&forwardTestDataplane{fullSyncErr: errors.New("full sync failed")})
	DeleteGroup(srv, remove)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `group deletion persisted but runtime is stopped`) {
		t.Fatalf("group delete runtime = %d %s", w.Code, w.Body.String())
	}
	groups, err := app.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range groups {
		if item.Name == "gone" {
			t.Fatal("deleted group remained persisted")
		}
	}

	first, err := app.CreateGroup(service.CreateGroupInput{Name: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.CreateGroup(service.CreateGroupInput{Name: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.CreateGroupLink(service.GroupLinkInput{FromGroupID: first.ID, ToGroupID: second.ID}); err != nil {
		t.Fatal(err)
	}
	app.Hub.SetNetworkRuntime(&forwardFailRuntime{startErr: errors.New("restart failed")})
	app.OnStarted(&forwardTestDataplane{fullSyncErr: errors.New("full sync failed")})
	removeLink, w := forwardContext(http.MethodDelete, "/api/groups/links", `{"from_group_id":`+strconv.Itoa(int(first.ID))+`,"to_group_id":`+strconv.Itoa(int(second.ID))+`}`)
	DeleteGroupLink(srv, removeLink)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `group-link deletion persisted but runtime is stopped`) {
		t.Fatalf("link delete runtime = %d %s", w.Code, w.Body.String())
	}
	links, err := store.ListGroupLinks()
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("deleted links remained persisted: %+v", links)
	}
}

func TestGroupLayoutSuppressesRepositoryUpdateFailure(t *testing.T) {
	srv, store, _ := setupForwardHandlerWithStore(t)
	app := srv.App
	db, err := sql.Open("sqlite", store.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("CREATE TRIGGER fail_group_update BEFORE UPDATE ON peer_groups BEGIN SELECT RAISE(ABORT, 'injected group update failure'); END")
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	layout, w := forwardContext(http.MethodPut, "/api/groups/layout", `{"groups":[{"id":1,"pos_x":3,"pos_y":4}]}`)
	UpdateGroupLayout(srv, layout)
	if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}` {
		t.Fatalf("layout update failure = %d %s", w.Code, w.Body.String())
	}
	groups, err := app.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	if groups[0].PosX != 0 || groups[0].PosY != 0 {
		t.Fatalf("layout changed despite repository failure: %+v", groups[0])
	}
}
