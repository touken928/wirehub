package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	dompolicy "github.com/touken928/wirehub/internal/domain/policy"
	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
	"github.com/touken928/wirehub/internal/repo"
	"github.com/touken928/wirehub/internal/service"
)

func forwardContext(method, path string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func setupForwardHandler(t *testing.T) (*Server, *service.App) {
	t.Helper()
	srv, store, _ := newTestServer(t)
	if err := store.Setup(repo.SetupInput{Endpoint: "example.com", Subnet: "100.127.0.0/24", AdminUsername: "admin", AdminPassword: "password123", ListenPort: 8443, MTU: 1420, StatusInterval: 1, ServerPrivateKey: "private", ServerPublicKey: "public"}); err != nil {
		t.Fatal(err)
	}
	return srv, srv.App
}

type forwardTestDataplane struct{ fullSyncErr error }

func (d *forwardTestDataplane) ApplyPolicy(dompolicy.AccessPolicySpec) error { return nil }
func (d *forwardTestDataplane) UpdateDNS(domainruntime.DNSCatalog, []domainruntime.WGPeer) error {
	return nil
}
func (d *forwardTestDataplane) FullSync(domainruntime.SyncBundle) error { return d.fullSyncErr }
func (d *forwardTestDataplane) GetStats() (map[string]domainruntime.PeerStats, error) {
	return nil, nil
}

type forwardFailRuntime struct{ startErr error }

func (r *forwardFailRuntime) Start(domainruntime.SyncBundle) error { return r.startErr }
func (r *forwardFailRuntime) Stop() error                          { return nil }
func (r *forwardFailRuntime) SetDNSUpstream([]string)              {}

func TestForwardHTTPCRUDAndListContract(t *testing.T) {
	srv, _ := setupForwardHandler(t)

	create, w := forwardContext(http.MethodPost, "/api/forwards", `{"name":"Web","listen_port":19090,"protocol":"tcp","target_host":"10.0.0.2","target_port":80}`)
	CreatePortForward(srv, create)
	if w.Code != http.StatusCreated || w.Body.String() != `{"id":1,"name":"Web","listen_port":19090,"protocol":"tcp","target_host":"10.0.0.2","target_port":80,"target_display":"10.0.0.2:80"}` {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}

	create, w = forwardContext(http.MethodPost, "/api/forwards", `{"name":"DNS","listen_port":19091,"protocol":"udp","target_host":"service.example.com","target_port":53}`)
	CreatePortForward(srv, create)
	if w.Code != http.StatusCreated {
		t.Fatalf("second create = %d %s", w.Code, w.Body.String())
	}
	occupied, w := forwardContext(http.MethodPost, "/api/forwards", `{"listen_port":19090,"protocol":"udp","target_host":"10.0.0.3","target_port":53}`)
	CreatePortForward(srv, occupied)
	if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"listen port 19090 is already in use"}` {
		t.Fatalf("occupied port = %d %s", w.Code, w.Body.String())
	}

	list, w := forwardContext(http.MethodGet, "/api/forwards", "")
	ListPortForwards(srv, list)
	wantList := `{"hub_ip":"100.127.0.1","hub_port":80,"rules":[{"id":1,"name":"Web","listen_port":19090,"protocol":"tcp","target_host":"10.0.0.2","target_port":80,"target_display":"10.0.0.2:80"},{"id":2,"name":"DNS","listen_port":19091,"protocol":"udp","target_host":"service.example.com","target_port":53,"target_display":"service.example.com:53"}]}`
	if w.Code != http.StatusOK || w.Body.String() != wantList {
		t.Fatalf("list = %d %s, want %s", w.Code, w.Body.String(), wantList)
	}

	update, w := forwardContext(http.MethodPut, "/api/forwards/1", `{"name":"Web2","listen_port":19092,"protocol":"TCP","target_host":"10.0.0.3","target_port":8080}`)
	update.Params = gin.Params{{Key: "id", Value: "1"}}
	UpdatePortForward(srv, update)
	if w.Code != http.StatusOK || w.Body.String() != `{"id":1,"name":"Web2","listen_port":19092,"protocol":"tcp","target_host":"10.0.0.3","target_port":8080,"target_display":"10.0.0.3:8080"}` {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}
	missingUpdate, w := forwardContext(http.MethodPut, "/api/forwards/99", `{"listen_port":19093,"protocol":"tcp","target_host":"10.0.0.4","target_port":80}`)
	missingUpdate.Params = gin.Params{{Key: "id", Value: "99"}}
	UpdatePortForward(srv, missingUpdate)
	if w.Code != http.StatusNotFound || w.Body.String() != `{"error":"forward not found"}` {
		t.Fatalf("missing update = %d %s", w.Code, w.Body.String())
	}

	remove, w := forwardContext(http.MethodDelete, "/api/forwards/1", "")
	remove.Params = gin.Params{{Key: "id", Value: "1"}}
	DeletePortForward(srv, remove)
	if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}` {
		t.Fatalf("delete = %d %s", w.Code, w.Body.String())
	}
	missing, w := forwardContext(http.MethodDelete, "/api/forwards/1", "")
	missing.Params = gin.Params{{Key: "id", Value: "1"}}
	DeletePortForward(srv, missing)
	if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}` {
		t.Fatalf("idempotent delete = %d %s", w.Code, w.Body.String())
	}

	remove, w = forwardContext(http.MethodDelete, "/api/forwards/2", "")
	remove.Params = gin.Params{{Key: "id", Value: "2"}}
	DeletePortForward(srv, remove)
	if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}` {
		t.Fatalf("second delete = %d %s", w.Code, w.Body.String())
	}
	empty, w := forwardContext(http.MethodGet, "/api/forwards", "")
	ListPortForwards(srv, empty)
	if w.Code != http.StatusOK || w.Body.String() != `{"hub_ip":"100.127.0.1","hub_port":80,"rules":[]}` {
		t.Fatalf("empty list = %d %s", w.Code, w.Body.String())
	}
	conflict, w := forwardContext(http.MethodPost, "/api/forwards", "")
	writeForwardErr(conflict, service.ErrPortForwardConflict)
	if w.Code != http.StatusConflict || w.Body.String() != `{"error":"listen port and protocol already in use"}` {
		t.Fatalf("conflict = %d %s", w.Code, w.Body.String())
	}
}

func TestForwardHTTPRuntimeFailureUses503JSON(t *testing.T) {
	srv, app := setupForwardHandler(t)
	app.Hub.SetNetworkRuntime(&forwardFailRuntime{startErr: errors.New("restart failed")})
	app.OnStarted(&forwardTestDataplane{fullSyncErr: errors.New("full sync failed")})

	create, w := forwardContext(http.MethodPost, "/api/forwards", `{"listen_port":19090,"protocol":"tcp","target_host":"10.0.0.2","target_port":80}`)
	CreatePortForward(srv, create)
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != `{"error":"port-forward creation persisted but runtime is stopped: full sync failed\nrestart failed"}` {
		t.Fatalf("runtime failure = %d %s", w.Code, w.Body.String())
	}
}
