package repo

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestCurrentSchemaConstraints(t *testing.T) {
	st := NewUnconfiguredStore(t, filepath.Join(t.TempDir(), "schema.db"))

	for _, column := range []struct {
		table   string
		name    string
		notNull bool
		pk      bool
	}{
		{"admins", "username", true, false},
		{"admins", "password_hash", true, false},
		{"settings", "upstream_dns", false, false},
		{"peers", "group_id", true, false},
		{"peers", "public_key", true, false},
		{"service_relays", "virtual_ip", true, false},
		{"relay_group_allows", "relay_id", false, true},
		{"relay_group_allows", "group_id", false, true},
	} {
		got, err := sqliteColumn(t, st.db, column.table, column.name)
		if err != nil {
			t.Fatal(err)
		}
		if (got.NotNull != 0) != column.notNull || (got.PK != 0) != column.pk {
			t.Fatalf("%s.%s metadata = notnull:%v pk:%v, want notnull:%v pk:%v", column.table, column.name, got.NotNull != 0, got.PK != 0, column.notNull, column.pk)
		}
	}

	assertUniqueIndex(t, st.db, "peers", []string{"name"})
	assertUniqueIndex(t, st.db, "peers", []string{"public_key"})
	assertUniqueIndex(t, st.db, "peers", []string{"wg_ip"})
	assertUniqueIndex(t, st.db, "port_forwards", []string{"listen_port", "protocol"})
	assertUniqueIndex(t, st.db, "service_relays", []string{"slug"})
	assertUniqueIndex(t, st.db, "service_relays", []string{"virtual_ip"})
}

func TestCurrentSchemaConstraintBehavior(t *testing.T) {
	db := newTestDB(t)
	group := &PeerGroup{Name: "group"}
	if err := db.Create(group).Error; err != nil {
		t.Fatal(err)
	}
	peer := &Peer{Name: "peer", PublicKey: "public", PrivateKey: "private", WGIP: "100.127.0.2", GroupID: group.ID}
	if err := db.Create(peer).Error; err != nil {
		t.Fatal(err)
	}
	for _, duplicate := range []*Peer{
		{Name: "peer", PublicKey: "public-2", PrivateKey: "private-2", WGIP: "100.127.0.3", GroupID: group.ID},
		{Name: "peer-2", PublicKey: "public", PrivateKey: "private-3", WGIP: "100.127.0.4", GroupID: group.ID},
		{Name: "peer-3", PublicKey: "public-3", PrivateKey: "private-4", WGIP: "100.127.0.2", GroupID: group.ID},
	} {
		if err := db.Create(duplicate).Error; err == nil {
			t.Fatalf("duplicate peer %#v was accepted", duplicate)
		}
	}
	for _, forward := range []*PortForward{
		{Name: "tcp", ListenPort: 9000, Protocol: "tcp", TargetHost: "10.0.0.1", TargetPort: 80},
		{Name: "udp", ListenPort: 9000, Protocol: "udp", TargetHost: "10.0.0.1", TargetPort: 80},
	} {
		if err := db.Create(forward).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&PortForward{ListenPort: 9000, Protocol: "tcp", TargetHost: "10.0.0.2", TargetPort: 81}).Error; err == nil {
		t.Fatal("duplicate listen port/protocol was accepted")
	}
	if err := db.Create(&MapGroupAllow{MapID: 1, GroupID: group.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&MapGroupAllow{MapID: 1, GroupID: group.ID}).Error; err == nil {
		t.Fatal("duplicate relay/group allow was accepted")
	}
}

func TestPreRefactorFixtureImportsAndMigratesPopulatedData(t *testing.T) {
	fixture := materializePreRefactorFixture(t)
	target := NewUnconfiguredStore(t, filepath.Join(t.TempDir(), "imported.db"))
	if err := target.ImportDatabase(fixture); err != nil {
		t.Fatalf("import pre-refactor fixture: %v", err)
	}

	settings, err := target.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ID != 1 || settings.ServerPublicKey != "legacy-server-public" || settings.ServerPrivateKey != "legacy-server-private" || settings.Endpoint != "legacy.example" || settings.ListenPort != 8443 || settings.WGSubnet != "100.127.0.0/24" || settings.HubIP != "100.127.0.1" || settings.DNSIP != "100.127.0.1" || settings.DNSSuffix != "wirehub" || settings.MTU != 1420 || settings.StatusInterval != 1 || len(settings.UpstreamDNS) != 2 || settings.UpstreamDNS[0] != "9.9.9.9" || settings.UpstreamDNS[1] != "1.1.1.1" {
		t.Fatalf("imported settings = %#v", settings)
	}
	admin, err := target.GetAdminByUsername("legacy-admin")
	if err != nil {
		t.Fatal(err)
	}
	if admin.ID != 1 || admin.PasswordHash != "legacy-hash" || admin.TokenVersion != 7 {
		t.Fatalf("imported admin = %#v", admin)
	}
	peers, err := target.ListPeers()
	if err != nil || len(peers) != 2 {
		t.Fatalf("imported peers = %d, err=%v", len(peers), err)
	}
	peerByName := make(map[string]Peer, len(peers))
	for _, peer := range peers {
		peerByName[peer.Name] = peer
	}
	for name, want := range map[string]Peer{
		"legacy-peer-a": {Name: "legacy-peer-a", PublicKey: "legacy-public-a", PrivateKey: "legacy-private-a", WGIP: "100.127.0.2", GroupID: 1, Enabled: true, DNSName: "peer-a", LastHandshake: 101, RxBytes: 11, TxBytes: 22, CreatedAt: time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)},
		"legacy-peer-b": {Name: "legacy-peer-b", PublicKey: "legacy-public-b", PrivateKey: "legacy-private-b", WGIP: "100.127.0.3", GroupID: 3, Enabled: true, DNSName: "peer-b", LastHandshake: 202, RxBytes: 33, TxBytes: 44, CreatedAt: time.Date(2024, time.February, 3, 4, 5, 6, 0, time.UTC)},
	} {
		got, ok := peerByName[name]
		if !ok {
			t.Fatalf("imported peer %q is missing", name)
		}
		if got.PublicKey != want.PublicKey || got.PrivateKey != want.PrivateKey || got.WGIP != want.WGIP || got.GroupID != want.GroupID || got.Enabled != want.Enabled || got.DNSName != want.DNSName || got.LastHandshake != want.LastHandshake || got.RxBytes != want.RxBytes || got.TxBytes != want.TxBytes || !got.CreatedAt.Equal(want.CreatedAt) {
			t.Fatalf("imported peer %q = %#v, want fields from %#v", name, got, want)
		}
	}
	groups, err := target.ListGroups()
	if err != nil || len(groups) != 3 {
		t.Fatalf("imported groups = %d, err=%v", len(groups), err)
	}
	groupByID := make(map[uint]PeerGroup, len(groups))
	for _, group := range groups {
		groupByID[group.ID] = group
	}
	for id, want := range map[uint]PeerGroup{
		1: {ID: 1, Name: "legacy-group", PosX: 10, PosY: 20, AllowIntraGroup: true},
		2: {ID: 2, Name: "second-group", PosX: -5, PosY: 6, AllowIntraGroup: false},
		3: {ID: 3, Name: "default", PosX: 0, PosY: 0, AllowIntraGroup: true},
	} {
		got, ok := groupByID[id]
		if !ok {
			t.Fatalf("imported group %d is missing", id)
		}
		if got.Name != want.Name || got.PosX != want.PosX || got.PosY != want.PosY || got.AllowIntraGroup != want.AllowIntraGroup {
			t.Fatalf("imported group %d = %#v, want %#v", id, got, want)
		}
	}
	links, err := target.ListGroupLinks()
	if err != nil || len(links) != 1 {
		t.Fatalf("imported group links = %d, err=%v", len(links), err)
	}
	if links[0].FromGroupID != 1 || links[0].ToGroupID != 2 || links[0].Bidirectional {
		t.Fatalf("imported directed link = %#v", links[0])
	}
	forwards, err := target.ListPortForwards()
	if err != nil || len(forwards) != 2 {
		t.Fatalf("imported port forwards = %d, err=%v", len(forwards), err)
	}
	if forwards[0].ListenPort != 9000 || forwards[0].Protocol != "tcp" || forwards[0].TargetHost != "10.0.0.10" || forwards[0].TargetPort != 80 || forwards[1].Protocol != "udp" || forwards[1].TargetHost != "10.0.0.11" || forwards[1].TargetPort != 53 {
		t.Fatalf("imported forwards = %#v", forwards)
	}
	maps, err := target.ListServiceMaps()
	if err != nil || len(maps) != 1 {
		t.Fatalf("imported service relays = %d, err=%v", len(maps), err)
	}
	if maps[0].ID != 1 || maps[0].Name != "legacy relay" || maps[0].Slug != "legacy-relay" || maps[0].TargetHost != "10.0.0.20" || maps[0].VirtualIP != "100.127.0.20" {
		t.Fatalf("imported service relay = %#v", maps[0])
	}
	allowed, err := target.ListMapGroupIDs(maps[0].ID)
	if err != nil || len(allowed) != 1 || allowed[0] != 1 {
		t.Fatalf("imported relay group allows = %#v, err=%v", allowed, err)
	}
	var records []DNSRecord
	if err := target.db.Order("id asc").Find(&records).Error; err != nil || len(records) != 2 {
		t.Fatalf("imported DNS records = %d, err=%v", len(records), err)
	}
	if records[0].Hostname != "peer-a" || records[0].IP != "100.127.0.2" || records[0].PeerID == nil || *records[0].PeerID != 1 || records[0].Manual {
		t.Fatalf("imported peer DNS record = %#v", records[0])
	}
	if records[1].Hostname != "manual" || records[1].IP != "100.127.0.10" || records[1].PeerID != nil || !records[1].Manual {
		t.Fatalf("imported manual DNS record = %#v", records[1])
	}
	assertUniqueIndex(t, target.db, "peers", []string{"name"})
	assertUniqueIndex(t, target.db, "peers", []string{"public_key"})
	assertUniqueIndex(t, target.db, "peers", []string{"wg_ip"})
	assertUniqueIndex(t, target.db, "port_forwards", []string{"listen_port", "protocol"})
	assertUniqueIndex(t, target.db, "service_relays", []string{"slug"})
	assertUniqueIndex(t, target.db, "service_relays", []string{"virtual_ip"})
	assertNoTable(t, target.db, "port_forward_dmzs")
	assertNoColumn(t, target.db, "peer_groups", "passive")
	if configured, err := target.IsConfigured(); err != nil || !configured {
		t.Fatalf("imported fixture is not configured: %v", err)
	}
}

type sqliteColumnInfo struct {
	NotNull int
	PK      int
}

func sqliteColumn(t *testing.T, db *gorm.DB, table, column string) (sqliteColumnInfo, error) {
	t.Helper()
	var info sqliteColumnInfo
	rows, err := db.Raw("SELECT \"notnull\", pk FROM pragma_table_info(?) WHERE name = ?", table, column).Rows()
	if err != nil {
		return info, err
	}
	defer rows.Close()
	if !rows.Next() {
		return info, fmt.Errorf("column %s.%s not found", table, column)
	}
	if err := rows.Scan(&info.NotNull, &info.PK); err != nil {
		return info, err
	}
	return info, rows.Err()
}

func assertUniqueIndex(t *testing.T, db *gorm.DB, table string, want []string) {
	t.Helper()
	rows, err := db.Raw("SELECT name FROM pragma_index_list(?) WHERE \"unique\" = 1", table).Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		var columns []string
		indexRows, err := db.Raw("SELECT name FROM pragma_index_info(?) ORDER BY seqno", name).Rows()
		if err != nil {
			t.Fatal(err)
		}
		for indexRows.Next() {
			var column string
			if err := indexRows.Scan(&column); err != nil {
				indexRows.Close()
				t.Fatal(err)
			}
			columns = append(columns, column)
		}
		indexRows.Close()
		if sameStrings(columns, want) {
			return
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("unique index on %s(%v) not found", table, want)
}

func assertNoTable(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	var count int
	if err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("obsolete table %q remains", table)
	}
}

func assertNoColumn(t *testing.T, db *gorm.DB, table, column string) {
	t.Helper()
	var count int
	if err := db.Raw("SELECT count(*) FROM pragma_table_info(?) WHERE name = ?", table, column).Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("obsolete column %s.%s remains", table, column)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func materializePreRefactorFixture(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	sqlText, err := os.ReadFile(filepath.Join(filepath.Dir(source), "testdata", "pre_refactor.sql"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pre-refactor.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(sqlText)); err != nil {
		db.Close()
		t.Fatalf("create pre-refactor fixture: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	st := NewUnconfiguredStore(t, filepath.Join(t.TempDir(), "models.db"))
	return st.db
}
