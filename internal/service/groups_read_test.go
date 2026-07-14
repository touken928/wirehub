package service

import (
	"reflect"
	"testing"
	"time"

	"github.com/touken928/wirehub/internal/repo"
)

func TestExportedGroupReadTypesContainNoRepoModelFields(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf(GroupView{}), reflect.TypeOf(GroupLinkView{}), reflect.TypeOf(GroupPeerView{}),
		reflect.TypeOf(GroupGraphGroupView{}), reflect.TypeOf(GroupGraphView{}),
	}
	seen := make(map[reflect.Type]bool)
	var visit func(reflect.Type)
	visit = func(typ reflect.Type) {
		if typ == nil || seen[typ] {
			return
		}
		seen[typ] = true
		if typ.PkgPath() == "github.com/touken928/wirehub/internal/repo" {
			t.Fatalf("group read type exposes repo model field type %s", typ)
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

func TestGroupReadViewsConvertCountsAndOrdering(t *testing.T) {
	app := testApp(t)
	late, err := app.store.CreateGroup("zeta", 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	early, err := app.store.CreateGroup("alpha", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := app.store.CreatePeer(&repo.Peer{Name: "old", PublicKey: "old-public", PrivateKey: "old-private", WGIP: "100.127.0.10", GroupID: late.ID, Enabled: true, CreatedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreatePeer(&repo.Peer{Name: "new", PublicKey: "new-public", PrivateKey: "new-private", WGIP: "100.127.0.11", GroupID: late.ID, Enabled: true, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.UpsertGroupLink(late.ID, early.ID, false); err != nil {
		t.Fatal(err)
	}

	groups, err := app.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 3 || groups[0].Name != "alpha" || groups[1].Name != "default" || groups[2].Name != "zeta" || groups[2].MemberCount != 2 {
		t.Fatalf("groups = %+v", groups)
	}

	graph, err := app.GroupGraph()
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Groups) != 3 || graph.Groups[0].Peers != nil || len(graph.Groups[2].Peers) != 2 {
		t.Fatalf("graph groups = %+v", graph.Groups)
	}
	if graph.Groups[2].Peers[0].Name != "new" || graph.Groups[2].Peers[1].Name != "old" {
		t.Fatalf("peer order = %+v", graph.Groups[2].Peers)
	}
	if graph.Groups[2].Peers[0].PublicKey != "new-public" {
		t.Fatalf("peer view = %+v", graph.Groups[2].Peers[0])
	}
	if len(graph.Links) != 1 || graph.Links[0].FromGroupID != late.ID || graph.Links[0].ToGroupID != early.ID {
		t.Fatalf("links = %+v", graph.Links)
	}
}
