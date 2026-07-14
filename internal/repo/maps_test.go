package repo

import (
	"path/filepath"
	"testing"

	"github.com/touken928/wirehub/internal/config"
)

func TestLegacyMapAllowedGroupOrderIsBackfilledFromRowOrder(t *testing.T) {
	dir := t.TempDir()
	st, err := New(&config.RuntimeConfig{DatabasePath: filepath.Join(dir, "wirehub.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.db.Create(&ServiceMap{ID: 1, Slug: "legacy", TargetHost: "10.0.0.1", VirtualIP: "100.127.0.20"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.db.Exec("DROP TABLE relay_group_allows").Error; err != nil {
		t.Fatal(err)
	}
	if err := st.db.Exec("CREATE TABLE relay_group_allows (relay_id INTEGER NOT NULL, group_id INTEGER NOT NULL, PRIMARY KEY (relay_id, group_id))").Error; err != nil {
		t.Fatal(err)
	}
	for _, groupID := range []uint{3, 1} {
		if err := st.db.Exec("INSERT INTO relay_group_allows (relay_id, group_id) VALUES (?, ?)", 1, groupID).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateDB(st.db); err != nil {
		t.Fatal(err)
	}
	if err := migrateDB(st.db); err != nil {
		t.Fatal(err)
	}
	got, err := st.ListMapGroupIDs(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 3 || got[1] != 1 {
		t.Fatalf("legacy allowed groups = %v, want [3 1]", got)
	}
	var positions []int
	if err := st.db.Table("relay_group_allows").Where("relay_id = ?", 1).Order("position").Pluck("position", &positions).Error; err != nil {
		t.Fatal(err)
	}
	if len(positions) != 2 || positions[0] != 0 || positions[1] != 1 {
		t.Fatalf("backfilled positions = %v, want [0 1]", positions)
	}
}

func TestCreateServiceMap_AllocatesVIPAndGroups(t *testing.T) {
	dir := t.TempDir()
	st, err := New(&config.RuntimeConfig{DatabasePath: filepath.Join(dir, "wirehub.db")})
	if err != nil {
		t.Fatal(err)
	}
	settings := &Settings{
		WGSubnet: "100.127.0.0/24",
		HubIP:    "100.127.0.1",
		DNSIP:    "100.127.0.1",
	}
	if err := st.db.Create(settings).Error; err != nil {
		t.Fatal(err)
	}
	g, err := st.CreateGroup("map-users", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	detail, err := st.CreateServiceMap(MapInput{
		Slug:          "intranet",
		TargetHost:    "127.0.0.1",
		AllowedGroups: []uint{g.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if detail.VirtualIP == "" || detail.VirtualIP == settings.HubIP {
		t.Fatalf("unexpected vip %q", detail.VirtualIP)
	}
	groups, err := st.ListMapGroupIDs(detail.ID)
	if err != nil || len(groups) != 1 || groups[0] != g.ID {
		t.Fatalf("groups = %v err=%v", groups, err)
	}
	ok, err := st.MapAllowedForPeer("100.127.0.2", detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("unknown peer should not be allowed")
	}
}
