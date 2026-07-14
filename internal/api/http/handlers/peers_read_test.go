package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	dompolicy "github.com/touken928/wirehub/internal/domain/policy"
	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
	"github.com/touken928/wirehub/internal/repo"
)

var peerResponseKeys = []string{"id", "name", "public_key", "wg_ip", "group_id", "enabled", "dns_name", "last_handshake", "rx_bytes", "tx_bytes", "created_at", "fqdn", "group_name"}

func peerHTTPContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func TestPeerHTTPContracts(t *testing.T) {
	srv, store, _ := newTestServer(t)
	if err := store.Setup(repo.SetupInput{Endpoint: "example.com", Subnet: "100.127.0.0/24", AdminUsername: "admin", AdminPassword: "password123", ListenPort: 8443, MTU: 1420, StatusInterval: 1, ServerPrivateKey: "server-private", ServerPublicKey: "server-public"}); err != nil {
		t.Fatal(err)
	}

	create, w := peerHTTPContext(http.MethodPost, "/api/peers", `{"name":"Alice","group_id":1}`)
	CreatePeer(srv, create)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	created := decodePeer(t, w)
	assertPeerKeys(t, created)
	assertPeerValues(t, created, 1, "alice", "public", "100.127.0.2", 1, true, "alice", 0, 0, 0, "alice.wirehub", "default")
	createdAt := created["created_at"]
	if _, err := time.Parse(time.RFC3339Nano, createdAt); err != nil {
		t.Fatalf("created_at = %q, want RFC3339Nano: %v", createdAt, err)
	}

	secondCreatedAt := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := store.CreatePeer(&repo.Peer{Name: "charlie", PublicKey: "charlie-public", PrivateKey: "charlie-private", WGIP: "100.127.0.3", GroupID: 1, Enabled: true, DNSName: "charlie", CreatedAt: secondCreatedAt}); err != nil {
		t.Fatal(err)
	}

	list, w := peerHTTPContext(http.MethodGet, "/api/peers", "")
	ListPeers(srv, list)
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d %s", w.Code, w.Body.String())
	}
	var listedRaw []map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &listedRaw); err != nil {
		t.Fatal(err)
	}
	listed := make([]map[string]string, len(listedRaw))
	for i, raw := range listedRaw {
		listed[i] = rawPeerValues(t, raw)
	}
	if len(listed) != 2 || listed[0]["name"] != "alice" || listed[1]["name"] != "charlie" {
		t.Fatalf("list ordering/content = %#v", listed)
	}
	for _, peer := range listed {
		if len(peer) != len(peerResponseKeys) {
			t.Fatalf("list fields = %#v", peer)
		}
		for _, key := range peerResponseKeys {
			if _, ok := peer[key]; !ok {
				t.Fatalf("list missing %q: %#v", key, peer)
			}
		}
		if _, leaked := peer["private_key"]; leaked {
			t.Fatal("list leaked private_key")
		}
	}
	if listed[0]["created_at"] != createdAt {
		t.Fatalf("list timestamp = %q, want %q", listed[0]["created_at"], createdAt)
	}
	assertPeerValues(t, listed[1], 2, "charlie", "charlie-public", "100.127.0.3", 1, true, "charlie", 0, 0, 0, "charlie.wirehub", "default")
	if listed[1]["created_at"] != secondCreatedAt.Format(time.RFC3339Nano) {
		t.Fatalf("second list timestamp = %q, want %q", listed[1]["created_at"], secondCreatedAt.Format(time.RFC3339Nano))
	}

	update, w := peerHTTPContext(http.MethodPut, "/api/peers/1", `{"name":"Beta","group_id":1}`)
	update.Params = gin.Params{{Key: "id", Value: "1"}}
	UpdatePeer(srv, update)
	if w.Code != http.StatusOK {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}
	updated := decodePeer(t, w)
	assertPeerValues(t, updated, 1, "beta", "public", "100.127.0.2", 1, true, "beta", 0, 0, 0, "beta.wirehub", "default")
	if updated["created_at"] != createdAt {
		t.Fatalf("update timestamp = %q, want %q", updated["created_at"], createdAt)
	}

	toggle, w := peerHTTPContext(http.MethodPost, "/api/peers/1/toggle", "")
	toggle.Params = gin.Params{{Key: "id", Value: "1"}}
	TogglePeer(srv, toggle)
	if w.Code != http.StatusOK {
		t.Fatalf("toggle = %d %s", w.Code, w.Body.String())
	}
	toggled := decodePeer(t, w)
	assertPeerValues(t, toggled, 1, "beta", "public", "100.127.0.2", 1, false, "beta", 0, 0, 0, "beta.wirehub", "default")

	config, w := peerHTTPContext(http.MethodGet, "/api/peers/1/config", "")
	config.Params = gin.Params{{Key: "id", Value: "1"}}
	PeerConfig(srv, config)
	wantConfig := "[Interface]\nPrivateKey = private\nAddress = 100.127.0.2/32\nDNS = 100.127.0.1\n# Hub web UI: http://hub.wirehub/\n\n[Peer]\nPublicKey = server-public\nEndpoint = example.com:8443\nPersistentKeepalive = 25\nAllowedIPs = 100.127.0.0/24\n"
	wantBody := map[string]string{"config": wantConfig, "filename": "beta.conf"}
	var gotBody map[string]string
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &gotBody) != nil || !reflect.DeepEqual(gotBody, wantBody) {
		t.Fatalf("config = %d %s", w.Code, w.Body.String())
	}
}

func TestPeerHTTPErrorContracts(t *testing.T) {
	srv, store, _ := newTestServer(t)
	if err := store.Setup(repo.SetupInput{Endpoint: "example.com", Subnet: "100.127.0.0/24", AdminUsername: "admin", AdminPassword: "password123", ListenPort: 8443, MTU: 1420, StatusInterval: 1, ServerPrivateKey: "server-private", ServerPublicKey: "server-public"}); err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string, id string, fn func(*Server, *gin.Context)) (int, string) {
		c, w := peerHTTPContext(method, path, body)
		if id != "" {
			c.Params = gin.Params{{Key: "id", Value: id}}
		}
		fn(srv, c)
		return w.Code, w.Body.String()
	}

	if code, body := call(http.MethodPost, "/api/peers", `{"name":"NoGroup","group_id":99}`, "", CreatePeer); code != http.StatusBadRequest || body != `{"error":"group not found"}` {
		t.Fatalf("missing group = %d %q", code, body)
	}
	if code, body := call(http.MethodPost, "/api/peers", `{"name":"Alice","group_id":1}`, "", CreatePeer); code != http.StatusCreated {
		t.Fatalf("seed peer = %d %q", code, body)
	}
	if code, body := call(http.MethodPost, "/api/peers", `{"name":"alice","group_id":1}`, "", CreatePeer); code != http.StatusBadRequest || body != `{"error":"hostname already exists"}` {
		t.Fatalf("duplicate hostname = %d %q", code, body)
	}
	if code, body := call(http.MethodPut, "/api/peers/404", `{"name":"missing"}`, "404", UpdatePeer); code != http.StatusNotFound || body != `{"error":"peer not found"}` {
		t.Fatalf("missing peer = %d %q", code, body)
	}
	if code, body := call(http.MethodPut, "/api/peers/1", `{}`, "1", UpdatePeer); code != http.StatusBadRequest || body != `{"error":"name or group_id is required"}` {
		t.Fatalf("empty update = %d %q", code, body)
	}
	if code, body := call(http.MethodGet, "/api/peers/404/config", "", "404", PeerConfig); code != http.StatusNotFound || body != `{"error":"peer not found"}` {
		t.Fatalf("missing config peer = %d %q", code, body)
	}
	settings, err := store.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Endpoint = ""
	if err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	if code, body := call(http.MethodGet, "/api/peers/1/config", "", "1", PeerConfig); code != http.StatusBadRequest || body != `{"error":"server endpoint is not configured"}` {
		t.Fatalf("config endpoint error = %d %q", code, body)
	}
}

func TestPeerHTTPSuppressesNotificationAfterReconciliationFailure(t *testing.T) {
	srv, store, _ := newTestServer(t)
	if err := store.Setup(repo.SetupInput{Endpoint: "example.com", Subnet: "100.127.0.0/24", AdminUsername: "admin", AdminPassword: "password123", ListenPort: 8443, MTU: 1420, StatusInterval: 1, ServerPrivateKey: "server-private", ServerPublicKey: "server-public"}); err != nil {
		t.Fatal(err)
	}
	net := &peerFailureNetwork{}
	srv.App.Hub.SetNetworkRuntime(net)
	srv.App.Status.SetNotifier(func() { net.notifications++ })
	srv.App.OnStarted(&peerFailureDataplane{})
	defer srv.App.OnStopped()

	create, w := peerHTTPContext(http.MethodPost, "/api/peers", `{"name":"Alice","group_id":1}`)
	CreatePeer(srv, create)
	want := `{"error":"peer creation persisted but runtime is stopped: injected sync failure\ninjected start failure"}`
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != want {
		t.Fatalf("reconciliation failure = %d %q, want %d %q", w.Code, w.Body.String(), http.StatusServiceUnavailable, want)
	}
	if net.notifications != 0 {
		t.Fatalf("notifications after failed reconciliation = %d, want 0", net.notifications)
	}
}

type peerFailureNetwork struct{ notifications int }

func (*peerFailureNetwork) Start(domainruntime.SyncBundle) error {
	return errors.New("injected start failure")
}
func (*peerFailureNetwork) Stop() error             { return nil }
func (*peerFailureNetwork) SetDNSUpstream([]string) {}

type peerFailureDataplane struct{}

func (*peerFailureDataplane) ApplyPolicy(dompolicy.AccessPolicySpec) error { return nil }
func (*peerFailureDataplane) UpdateDNS(domainruntime.DNSCatalog, []domainruntime.WGPeer) error {
	return nil
}
func (*peerFailureDataplane) FullSync(domainruntime.SyncBundle) error {
	return errors.New("injected sync failure")
}
func (*peerFailureDataplane) GetStats() (map[string]domainruntime.PeerStats, error) {
	return nil, nil
}

func decodePeer(t *testing.T, w *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	return rawPeerValues(t, raw)
}

func rawPeerValues(t *testing.T, raw map[string]json.RawMessage) map[string]string {
	t.Helper()
	peer := make(map[string]string, len(raw))
	for key, value := range raw {
		var text string
		if len(value) > 0 && value[0] == '"' {
			if err := json.Unmarshal(value, &text); err != nil {
				t.Fatal(err)
			}
			peer[key] = text
		} else {
			peer[key] = string(value)
		}
	}
	return peer
}

func assertPeerKeys(t *testing.T, peer map[string]string) {
	t.Helper()
	if len(peer) != len(peerResponseKeys) {
		t.Fatalf("peer fields = %#v", peer)
	}
	for _, key := range peerResponseKeys {
		if _, ok := peer[key]; !ok {
			t.Fatalf("peer missing %q: %#v", key, peer)
		}
	}
	if _, leaked := peer["private_key"]; leaked {
		t.Fatal("peer leaked private_key")
	}
}

func assertPeerValues(t *testing.T, peer map[string]string, id uint, name, publicKey, wgIP string, groupID uint, enabled bool, dns string, handshake, rx, tx int64, fqdn, groupName string) {
	t.Helper()
	want := map[string]string{
		"id": strconv.FormatUint(uint64(id), 10), "name": name, "public_key": publicKey, "wg_ip": wgIP, "group_id": strconv.FormatUint(uint64(groupID), 10),
		"enabled": strconv.FormatBool(enabled), "dns_name": dns, "last_handshake": strconv.FormatInt(handshake, 10), "rx_bytes": strconv.FormatInt(rx, 10), "tx_bytes": strconv.FormatInt(tx, 10),
		"fqdn": fqdn, "group_name": groupName,
	}
	for key, value := range want {
		if peer[key] != value {
			t.Fatalf("peer[%q] = %q, want %q", key, peer[key], value)
		}
	}
}
