package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/touken928/wirehub/internal/service"
)

func TestMapHTTPCRUDOrderingAndEmptyListContract(t *testing.T) {
	srv, app := setupForwardHandler(t)
	group, err := app.CreateGroup(service.CreateGroupInput{Name: "restricted"})
	if err != nil {
		t.Fatal(err)
	}

	create, w := forwardContext(http.MethodPost, "/api/maps", `{"name":"Zulu","slug":"zulu","target_host":"10.0.0.2","allowed_group_ids":[2,1]}`)
	CreateMap(srv, create)
	if w.Code != http.StatusCreated || w.Body.String() != `{"id":1,"name":"Zulu","slug":"zulu","target_host":"10.0.0.2","virtual_ip":"100.127.0.2","target_display":"10.0.0.2","allowed_group_ids":[2,1],"fqdn":"zulu.wirehub"}` {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	if group.ID != 2 {
		t.Fatalf("restricted group id = %d, want 2", group.ID)
	}

	create, w = forwardContext(http.MethodPost, "/api/maps", `{"name":"Alpha","slug":"alpha","target_host":"10.0.0.3","allowed_group_ids":[1]}`)
	CreateMap(srv, create)
	if w.Code != http.StatusCreated {
		t.Fatalf("second create = %d %s", w.Code, w.Body.String())
	}

	list, w := forwardContext(http.MethodGet, "/api/maps", "")
	ListMaps(srv, list)
	want := `{"maps":[{"id":2,"name":"Alpha","slug":"alpha","target_host":"10.0.0.3","virtual_ip":"100.127.0.3","target_display":"10.0.0.3","allowed_group_ids":[1],"fqdn":"alpha.wirehub"},{"id":1,"name":"Zulu","slug":"zulu","target_host":"10.0.0.2","virtual_ip":"100.127.0.2","target_display":"10.0.0.2","allowed_group_ids":[2,1],"fqdn":"zulu.wirehub"}]}`
	if w.Code != http.StatusOK || w.Body.String() != want {
		t.Fatalf("list = %d %s, want %s", w.Code, w.Body.String(), want)
	}

	update, w := forwardContext(http.MethodPut, "/api/maps/1", `{"name":"Zulu 2","slug":"zulu-2","target_host":"10.0.0.4","allowed_group_ids":[1,2]}`)
	update.Params = mapID(1)
	UpdateMap(srv, update)
	if w.Code != http.StatusOK || w.Body.String() != `{"id":1,"name":"Zulu 2","slug":"zulu-2","target_host":"10.0.0.4","virtual_ip":"100.127.0.2","target_display":"10.0.0.4","allowed_group_ids":[1,2],"fqdn":"zulu-2.wirehub"}` {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}

	remove, w := forwardContext(http.MethodDelete, "/api/maps/1", "")
	remove.Params = mapID(1)
	DeleteMap(srv, remove)
	if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}` {
		t.Fatalf("delete = %d %s", w.Code, w.Body.String())
	}
	remove, w = forwardContext(http.MethodDelete, "/api/maps/2", "")
	remove.Params = mapID(2)
	DeleteMap(srv, remove)
	empty, w := forwardContext(http.MethodGet, "/api/maps", "")
	ListMaps(srv, empty)
	if w.Code != http.StatusOK || w.Body.String() != `{"maps":[]}` {
		t.Fatalf("empty list = %d %s", w.Code, w.Body.String())
	}
}

func TestMapHTTPErrorAndRuntimeContracts(t *testing.T) {
	srv, store, _ := newTestServer(t)
	if err := store.Setup(testSetupInput()); err != nil {
		t.Fatal(err)
	}
	create := func(body string) (*gin.Context, *httptest.ResponseRecorder) {
		return forwardContext(http.MethodPost, "/api/maps", body)
	}

	c, w := create(`{"slug":"missing-group","target_host":"10.0.0.2","allowed_group_ids":[99]}`)
	CreateMap(srv, c)
	if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"allowed group not found"}` {
		t.Fatalf("missing group = %d %s", w.Code, w.Body.String())
	}

	settings, err := store.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.WGSubnet = "10.0.0.0/31"
	settings.HubIP = "10.0.0.1"
	settings.DNSIP = "10.0.0.1"
	if err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	c, w = create(`{"slug":"no-ip","target_host":"10.0.0.2","allowed_group_ids":[1]}`)
	CreateMap(srv, c)
	if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"no map virtual ip available in subnet"}` {
		t.Fatalf("allocation = %d %s", w.Code, w.Body.String())
	}

	// Restore a usable subnet before exercising conflict and reconciliation.
	settings.WGSubnet = "100.127.0.0/24"
	settings.HubIP = "100.127.0.1"
	settings.DNSIP = "100.127.0.1"
	if err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	c, w = create(`{"slug":"same","target_host":"10.0.0.2","allowed_group_ids":[1]}`)
	CreateMap(srv, c)
	c, w = create(`{"slug":"same","target_host":"10.0.0.3","allowed_group_ids":[1]}`)
	CreateMap(srv, c)
	if w.Code != http.StatusConflict || w.Body.String() != `{"error":"map slug already in use"}` {
		t.Fatalf("conflict = %d %s", w.Code, w.Body.String())
	}

	srv.App.Hub.SetNetworkRuntime(&forwardFailRuntime{startErr: errors.New("restart failed")})
	srv.App.OnStarted(&forwardTestDataplane{fullSyncErr: errors.New("full sync failed")})
	c, w = create(`{"slug":"runtime","target_host":"10.0.0.4","allowed_group_ids":[1]}`)
	CreateMap(srv, c)
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != `{"error":"map creation persisted but runtime is stopped: full sync failed\nrestart failed"}` {
		t.Fatalf("runtime = %d %s", w.Code, w.Body.String())
	}
	maps, err := srv.App.ListMapDetails()
	if err != nil || len(maps) != 2 {
		t.Fatalf("persisted maps = %d, err=%v", len(maps), err)
	}
}

func mapID(id uint) gin.Params {
	return gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(id), 10)}}
}
