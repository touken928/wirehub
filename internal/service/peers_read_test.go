package service

import (
	"reflect"
	"strings"
	"testing"
)

func TestPeerServiceViewsEnrichAndKeepSecretsInternal(t *testing.T) {
	app := testApp(t)
	group, err := app.store.CreateGroup("users", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.CreatePeer(CreatePeerInput{Name: "Alice", GroupID: group.ID})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "alice" || created.GroupName != "users" || created.FQDN != "alice.wirehub" || created.PublicKey == "" {
		t.Fatalf("created view = %+v", created)
	}
	peers, err := app.ListPeers()
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || peers[0].ID != created.ID || peers[0].Name != created.Name || peers[0].GroupName != created.GroupName || peers[0].FQDN != created.FQDN {
		t.Fatalf("peers = %+v, want %v", peers, created)
	}
	toggled, err := app.TogglePeer(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if toggled.Enabled == created.Enabled || toggled.GroupName != "users" {
		t.Fatalf("toggled view = %+v", toggled)
	}
	config, err := app.ClientConfig(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if config.Filename != "alice.conf" || !strings.Contains(config.Config, "PrivateKey =") {
		t.Fatalf("client config result = %+v", config)
	}
}

func TestExportedPeerReadTypesContainNoRepoModelFields(t *testing.T) {
	types := []reflect.Type{reflect.TypeOf(PeerView{}), reflect.TypeOf(CreatePeerInput{}), reflect.TypeOf(UpdatePeerInput{}), reflect.TypeOf(ClientConfigResult{})}
	seen := make(map[reflect.Type]bool)
	var visit func(reflect.Type)
	visit = func(typ reflect.Type) {
		if typ == nil || seen[typ] {
			return
		}
		seen[typ] = true
		if typ.PkgPath() == "github.com/touken928/wirehub/internal/repo" {
			t.Fatalf("peer read type exposes repo model field type %s", typ)
		}
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			visit(typ.Elem())
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				visit(typ.Field(i).Type)
			}
		}
	}
	for _, typ := range types {
		visit(typ)
	}
}
