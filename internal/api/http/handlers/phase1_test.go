package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/touken928/wirehub/internal/api/http/auth"
	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
	"github.com/touken928/wirehub/internal/repo"
)

func TestLoginAndProtectedSettingsCharacterization(t *testing.T) {
	srv, st, _ := newTestServer(t)
	if err := st.Setup(testSetupInput()); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService("test-secret", srv.App)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"admin","password":"testpass123"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("auth", authSvc)
	Login(srv, c)
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", w.Code, w.Body.String())
	}
	var loginBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &loginBody); err != nil {
		t.Fatal(err)
	}
	if loginBody.Token == "" {
		t.Fatal("login response token is empty")
	}
	claims, err := authSvc.ParseToken(loginBody.Token)
	if err != nil {
		t.Fatal(err)
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	c.Set("admin_id", claims.AdminID)
	c.Set("username", claims.Username)
	GetSettings(srv, c)
	if w.Code != http.StatusOK {
		t.Fatalf("settings status = %d, body = %s", w.Code, w.Body.String())
	}
	var settings struct {
		Endpoint        string   `json:"endpoint"`
		Subnet          string   `json:"subnet"`
		AdminUsername   string   `json:"admin_username"`
		HubIP           string   `json:"hub_ip"`
		DNSIP           string   `json:"dns_ip"`
		DNSSuffix       string   `json:"dns_suffix"`
		ListenPort      int      `json:"listen_port"`
		ServerPublicKey string   `json:"server_public_key"`
		MTU             int      `json:"mtu"`
		StatusInterval  int      `json:"status_interval"`
		UpstreamDNS     []string `json:"upstream_dns"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	wantSettings := struct {
		Endpoint        string   `json:"endpoint"`
		Subnet          string   `json:"subnet"`
		AdminUsername   string   `json:"admin_username"`
		HubIP           string   `json:"hub_ip"`
		DNSIP           string   `json:"dns_ip"`
		DNSSuffix       string   `json:"dns_suffix"`
		ListenPort      int      `json:"listen_port"`
		ServerPublicKey string   `json:"server_public_key"`
		MTU             int      `json:"mtu"`
		StatusInterval  int      `json:"status_interval"`
		UpstreamDNS     []string `json:"upstream_dns"`
	}{Endpoint: "example.com", Subnet: "100.127.0.0/24", AdminUsername: "admin", HubIP: "100.127.0.1", DNSIP: "100.127.0.1", DNSSuffix: "wirehub", ListenPort: 8443, ServerPublicKey: "public", MTU: 1420, StatusInterval: 1}
	if !reflect.DeepEqual(settings, wantSettings) {
		t.Fatalf("settings JSON decoded as %+v, want %+v", settings, wantSettings)
	}
}

func TestStatusWSQueryTokenAndAuthorizationFallback(t *testing.T) {
	srv, st, _ := newTestServer(t)
	if err := st.Setup(testSetupInput()); err != nil {
		t.Fatal(err)
	}
	if err := st.CreatePeer(&repo.Peer{Name: "status-peer", PublicKey: "status-public", PrivateKey: "status-private", WGIP: "100.127.0.2", GroupID: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService("test-secret", srv.App)
	token, err := authSvc.Login("admin", "testpass123")
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("auth", authSvc); c.Next() })
	r.GET("/api/ws/status", func(c *gin.Context) { StatusWS(srv, c) })
	httpSrv := httptest.NewServer(r)
	defer httpSrv.Close()
	wsURL := "ws" + httpSrv.URL[len("http"):]
	for _, tc := range []struct {
		name   string
		url    string
		header string
	}{
		{name: "query token", url: wsURL + "/api/ws/status?token=" + token},
		{name: "authorization fallback", url: wsURL + "/api/ws/status", header: "Bearer " + token},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := http.Header{}
			if tc.header != "" {
				headers.Set("Authorization", tc.header)
			}
			conn, resp, err := websocket.DefaultDialer.Dial(tc.url, headers)
			if err != nil {
				if resp != nil {
					t.Fatalf("websocket dial: %v (status %d)", err, resp.StatusCode)
				}
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			_, payload, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("read initial status snapshot: %v", err)
			}
			var message struct {
				Type     string            `json:"type"`
				Peers    []json.RawMessage `json:"peers"`
				Settings map[string]any    `json:"settings"`
			}
			if err := json.Unmarshal(payload, &message); err != nil {
				t.Fatalf("invalid status JSON: %v", err)
			}
			if message.Type != "status" || len(message.Peers) != 1 || message.Settings == nil {
				t.Fatalf("unexpected status shape: %s", payload)
			}
			var statusPeer map[string]json.RawMessage
			if err := json.Unmarshal(message.Peers[0], &statusPeer); err != nil {
				t.Fatal(err)
			}
			assertRawKeys(t, statusPeer, "id", "name", "fqdn", "wg_ip", "group_id", "group_name", "enabled", "last_handshake", "rx_bytes", "tx_bytes", "online")
			for key, want := range map[string]string{"name": `"status-peer"`, "fqdn": `"status-peer.wirehub"`, "wg_ip": `"100.127.0.2"`, "group_id": `1`, "enabled": `true`, "last_handshake": `0`, "rx_bytes": `0`, "tx_bytes": `0`, "online": `false`} {
				if string(statusPeer[key]) != want {
					t.Fatalf("status peer %s = %s, want %s", key, statusPeer[key], want)
				}
			}
			assertJSONKeys(t, payload, "type", "peers", "settings")
			assertJSONKeys(t, mustRawObject(t, message.Settings), "id", "server_public_key", "endpoint", "listen_port", "wg_subnet", "hub_ip", "dns_ip", "dns_suffix", "mtu", "status_interval", "upstream_dns")
			if _, private := message.Settings["server_private_key"]; private {
				t.Fatalf("status settings leaked server_private_key: %s", payload)
			}
		})
	}
}

func TestRepresentativeJSONContractsAndErrors(t *testing.T) {
	srv, st, _ := newTestServer(t)

	if err := st.Setup(testSetupInput()); err != nil {
		t.Fatal(err)
	}
	settingsW := httptest.NewRecorder()
	settingsC, _ := gin.CreateTestContext(settingsW)
	settingsC.Request = httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	GetSettings(srv, settingsC)
	var settings map[string]any
	if err := json.Unmarshal(settingsW.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	expectedSettings := map[string]any{
		"endpoint": "example.com", "subnet": "100.127.0.0/24", "admin_username": "admin",
		"hub_ip": "100.127.0.1", "dns_ip": "100.127.0.1", "dns_suffix": "wirehub",
		"listen_port": float64(8443), "server_public_key": "public", "mtu": float64(1420), "status_interval": float64(1),
	}
	for key, value := range expectedSettings {
		if settings[key] != value {
			t.Fatalf("settings[%q] = %#v, want %#v; body=%s", key, settings[key], value, settingsW.Body.String())
		}
	}
	if _, leaked := settings["server_private_key"]; leaked {
		t.Fatal("settings leaked server_private_key")
	}
	assertJSONKeys(t, settingsW.Body.Bytes(), "endpoint", "subnet", "admin_username", "hub_ip", "dns_ip", "dns_suffix", "listen_port", "server_public_key", "mtu", "status_interval", "upstream_dns")

	for _, tc := range []struct {
		name, target, body, want string
		code                     int
		call                     func(*gin.Context)
	}{
		{name: "peer invalid id", target: "/api/peers/nope", body: "", code: http.StatusBadRequest, want: `{"error":"invalid id"}`},
		{name: "group malformed", target: "/api/groups", body: "{}", code: http.StatusBadRequest, want: `{"error":"Key: 'createGroupRequest.Name' Error:Field validation for 'Name' failed on the 'required' tag"}`},
		{name: "map malformed", target: "/api/maps", body: "{}", code: http.StatusBadRequest, want: `{"error":"Key: 'mapRequest.Slug' Error:Field validation for 'Slug' failed on the 'required' tag\nKey: 'mapRequest.TargetHost' Error:Field validation for 'TargetHost' failed on the 'required' tag\nKey: 'mapRequest.AllowedGroupIDs' Error:Field validation for 'AllowedGroupIDs' failed on the 'required' tag"}`},
		{name: "forward malformed", target: "/api/forwards", body: "{}", code: http.StatusBadRequest, want: `{"error":"Key: 'portForwardRequest.ListenPort' Error:Field validation for 'ListenPort' failed on the 'required' tag\nKey: 'portForwardRequest.Protocol' Error:Field validation for 'Protocol' failed on the 'required' tag\nKey: 'portForwardRequest.TargetHost' Error:Field validation for 'TargetHost' failed on the 'required' tag\nKey: 'portForwardRequest.TargetPort' Error:Field validation for 'TargetPort' failed on the 'required' tag"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, tc.target, bytes.NewBufferString(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			switch tc.name {
			case "peer invalid id":
				tc.call = func(c *gin.Context) { UpdatePeer(srv, c) }
			case "group malformed":
				tc.call = func(c *gin.Context) { CreateGroup(srv, c) }
			case "map malformed":
				tc.call = func(c *gin.Context) { CreateMap(srv, c) }
			case "forward malformed":
				tc.call = func(c *gin.Context) { CreatePortForward(srv, c) }
			}
			if tc.name == "peer invalid id" {
				c.Params = gin.Params{{Key: "id", Value: "nope"}}
			}
			tc.call(c)
			if w.Code != tc.code || w.Body.String() != tc.want {
				t.Fatalf("got %d %q, want %d %q", w.Code, w.Body.String(), tc.code, tc.want)
			}
		})
	}
}

func assertJSONKeys(t *testing.T, data []byte, want ...string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	wantSet := make(map[string]bool, len(want))
	for _, key := range want {
		wantSet[key] = true
	}
	if len(object) != len(wantSet) {
		t.Fatalf("JSON keys = %v, want exactly %v", keys(object), want)
	}
	for key := range object {
		if !wantSet[key] {
			t.Fatalf("unexpected JSON key %q", key)
		}
	}
}

func assertRawKeys(t *testing.T, object map[string]json.RawMessage, want ...string) {
	t.Helper()
	if len(object) != len(want) {
		t.Fatalf("JSON keys = %v, want exactly %v", keys(object), want)
	}
	wanted := make(map[string]bool, len(want))
	for _, key := range want {
		wanted[key] = true
	}
	for key := range object {
		if !wanted[key] {
			t.Fatalf("unexpected JSON key %q", key)
		}
	}
}

func keys(object map[string]json.RawMessage) []string {
	out := make([]string, 0, len(object))
	for key := range object {
		out = append(out, key)
	}
	return out
}

func jsonNumber(raw json.RawMessage) float64 {
	var value float64
	_ = json.Unmarshal(raw, &value)
	return value
}

func mustRawObject(t *testing.T, object map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type stoppedRuntime struct{ mockNetworkRuntime }

func (m *stoppedRuntime) Start(domainruntime.SyncBundle) error {
	return fmt.Errorf("injected runtime start failure")
}

func TestUpdateSettingsStoppedRuntimeMapsTo503(t *testing.T) {
	srv, st, _ := newTestServer(t)
	if err := st.Setup(testSetupInput()); err != nil {
		t.Fatal(err)
	}
	srv.App.Hub.SetNetworkRuntime(&stoppedRuntime{})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(`{"mtu":1500,"status_interval":1,"upstream_dns":[]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	UpdateSettings(srv, c)
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != `{"error":"mutable settings update persisted but runtime is stopped: injected runtime start failure"}` {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
}

func TestStatusWSUnauthorizedBodyMatrix(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for _, tc := range []struct {
		name, target, header, body string
	}{
		{name: "missing", target: "/api/ws/status", body: `{"error":"missing token"}`},
		{name: "invalid query", target: "/api/ws/status?token=bad", body: `{"error":"invalid token"}`},
		{name: "invalid header", target: "/api/ws/status", header: "Bearer bad", body: `{"error":"invalid token"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, tc.target, nil)
			c.Request.Header.Set("Authorization", tc.header)
			c.Set("auth", auth.NewService("test-secret", srv.App))
			StatusWS(srv, c)
			if w.Code != http.StatusUnauthorized || w.Body.String() != tc.body {
				t.Fatalf("got %d %q, want 401 %q", w.Code, w.Body.String(), tc.body)
			}
		})
	}
}

func testSetupInput() repo.SetupInput {
	return repo.SetupInput{
		Endpoint: "example.com", Subnet: "100.127.0.0/24",
		AdminUsername: "admin", AdminPassword: "testpass123",
		ListenPort: 8443, MTU: 1420, StatusInterval: 1,
		ServerPrivateKey: "private", ServerPublicKey: "public",
	}
}
