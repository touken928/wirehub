package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/touken928/wirehub/internal/repo"
)

func TestGroupReadHTTPEmptyAndPopulatedContracts(t *testing.T) {
	srv, store, _ := newTestServer(t)
	if err := store.Setup(repo.SetupInput{Endpoint: "example.com", Subnet: "100.127.0.0/24", AdminUsername: "admin", AdminPassword: "password123", ListenPort: 8443, MTU: 1420, StatusInterval: 1, ServerPrivateKey: "private", ServerPublicKey: "public"}); err != nil {
		t.Fatal(err)
	}

	list := func() (int, string) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/groups", nil)
		ListGroups(srv, c)
		return w.Code, w.Body.String()
	}
	graph := func() (int, string) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/groups/graph", nil)
		GroupGraph(srv, c)
		return w.Code, w.Body.String()
	}

	if code, body := list(); code != http.StatusOK || body != `[{"id":1,"name":"default","pos_x":0,"pos_y":0,"allow_intra_group":true,"MemberCount":0}]` {
		t.Fatalf("empty list = %d %s", code, body)
	}
	if code, body := graph(); code != http.StatusOK || body != `{"groups":[{"id":1,"name":"default","pos_x":0,"pos_y":0,"allow_intra_group":true,"member_count":0,"peers":null}],"links":[]}` {
		t.Fatalf("empty graph = %d %s", code, body)
	}

	if _, err := store.CreateGroup("alpha", 1, 2); err != nil {
		t.Fatal(err)
	}
	if code, body := list(); code != http.StatusOK || body != `[{"id":2,"name":"alpha","pos_x":1,"pos_y":2,"allow_intra_group":true,"MemberCount":0},{"id":1,"name":"default","pos_x":0,"pos_y":0,"allow_intra_group":true,"MemberCount":0}]` {
		t.Fatalf("populated list = %d %s", code, body)
	}
	if code, body := graph(); code != http.StatusOK || body != `{"groups":[{"id":2,"name":"alpha","pos_x":1,"pos_y":2,"allow_intra_group":true,"member_count":0,"peers":null},{"id":1,"name":"default","pos_x":0,"pos_y":0,"allow_intra_group":true,"member_count":0,"peers":null}],"links":[]}` {
		t.Fatalf("populated graph = %d %s", code, body)
	}
}

func TestGroupGraphHTTPPopulatedContract(t *testing.T) {
	srv, store, _ := newTestServer(t)
	if err := store.Setup(repo.SetupInput{Endpoint: "example.com", Subnet: "100.127.0.0/24", AdminUsername: "admin", AdminPassword: "password123", ListenPort: 8443, MTU: 1420, StatusInterval: 1, ServerPrivateKey: "private", ServerPublicKey: "public"}); err != nil {
		t.Fatal(err)
	}
	alpha, err := store.CreateGroup("alpha", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	beta, err := store.CreateGroup("beta", 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertGroupLink(alpha.ID, beta.ID, true); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	newTime := oldTime.Add(time.Hour)
	for _, peer := range []*repo.Peer{
		{Name: "old", PublicKey: "old-public", PrivateKey: "old-private", WGIP: "100.127.0.10", GroupID: alpha.ID, Enabled: true, DNSName: "old", LastHandshake: 11, RxBytes: 1, TxBytes: 2, CreatedAt: oldTime},
		{Name: "new", PublicKey: "new-public", PrivateKey: "new-private", WGIP: "100.127.0.11", GroupID: alpha.ID, Enabled: true, DNSName: "new", LastHandshake: 22, RxBytes: 3, TxBytes: 4, CreatedAt: newTime},
		{Name: "beta-peer", PublicKey: "beta-public", PrivateKey: "beta-private", WGIP: "100.127.0.12", GroupID: beta.ID, Enabled: true, DNSName: "beta-peer", LastHandshake: 33, RxBytes: 5, TxBytes: 6, CreatedAt: oldTime.Add(2 * time.Hour)},
	} {
		if err := store.CreatePeer(peer); err != nil {
			t.Fatal(err)
		}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/groups/graph", nil)
	GroupGraph(srv, c)
	want := `{"groups":[{"id":2,"name":"alpha","pos_x":1,"pos_y":2,"allow_intra_group":true,"member_count":2,"peers":[{"id":2,"name":"new","public_key":"new-public","wg_ip":"100.127.0.11","group_id":2,"enabled":true,"dns_name":"new","last_handshake":22,"rx_bytes":3,"tx_bytes":4,"created_at":"2025-01-02T04:04:05Z","fqdn":"new.wirehub"},{"id":1,"name":"old","public_key":"old-public","wg_ip":"100.127.0.10","group_id":2,"enabled":true,"dns_name":"old","last_handshake":11,"rx_bytes":1,"tx_bytes":2,"created_at":"2025-01-02T03:04:05Z","fqdn":"old.wirehub"}]},{"id":3,"name":"beta","pos_x":3,"pos_y":4,"allow_intra_group":true,"member_count":1,"peers":[{"id":3,"name":"beta-peer","public_key":"beta-public","wg_ip":"100.127.0.12","group_id":3,"enabled":true,"dns_name":"beta-peer","last_handshake":33,"rx_bytes":5,"tx_bytes":6,"created_at":"2025-01-02T05:04:05Z","fqdn":"beta-peer.wirehub"}]},{"id":1,"name":"default","pos_x":0,"pos_y":0,"allow_intra_group":true,"member_count":0,"peers":null}],"links":[{"id":1,"from_group_id":2,"to_group_id":3,"bidirectional":true}]}`
	if w.Code != http.StatusOK || w.Body.String() != want {
		t.Fatalf("graph = %d %s, want %s", w.Code, w.Body.String(), want)
	}
}
