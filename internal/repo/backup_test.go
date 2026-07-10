package repo

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/touken928/wirehub/internal/config"
	"gorm.io/gorm"
)

func configuredStore(t *testing.T, path string) *Store {
	t.Helper()
	st, err := New(&config.RuntimeConfig{DatabasePath: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Setup(SetupInput{
		Endpoint: "example.com", Subnet: "100.127.0.0/24",
		AdminUsername: "admin", AdminPassword: "password123",
		ListenPort: 8443, MTU: 1420, StatusInterval: 1,
		ServerPrivateKey: "private", ServerPublicKey: "public",
	}); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestExportDatabaseIsSQLiteSnapshot(t *testing.T) {
	src := configuredStore(t, filepath.Join(t.TempDir(), "wirehub.db"))
	var snapshot bytes.Buffer
	if err := src.ExportDatabase(&snapshot); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "snapshot.db")
	if err := os.WriteFile(path, snapshot.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWireHubDatabase(path); err != nil {
		t.Fatalf("snapshot validation failed: %v", err)
	}
}

func TestImportDatabaseRejectedFileRestoresLiveStore(t *testing.T) {
	dir := t.TempDir()
	st := NewUnconfiguredStore(t, filepath.Join(dir, "wirehub.db"))
	bad := filepath.Join(dir, "bad.db")
	if err := os.WriteFile(bad, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := st.ImportDatabase(bad); err == nil {
		t.Fatal("expected invalid import to fail")
	}
	configured, err := st.IsConfigured()
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		t.Fatal("rejected import changed the live database")
	}
}

func TestImportDatabaseConcurrentStoreUsage(t *testing.T) {
	dir := t.TempDir()
	src := configuredStore(t, filepath.Join(t.TempDir(), "source.db"))
	var snapshot bytes.Buffer
	if err := src.ExportDatabase(&snapshot); err != nil {
		t.Fatal(err)
	}
	importPath := filepath.Join(dir, "upload.db")
	if err := os.WriteFile(importPath, snapshot.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	target := NewUnconfiguredStore(t, filepath.Join(dir, "wirehub.db"))
	var wg sync.WaitGroup
	results := make(chan error, 8*100)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, err := target.IsConfigured(); err != nil {
					results <- err
				}
				if _, err := target.GetSettings(); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					results <- err
				}
			}
		}()
	}
	if err := target.ImportDatabase(importPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(importPath); err != nil {
		t.Fatalf("import consumed source: %v", err)
	}
	wg.Wait()
	close(results)
	for err := range results {
		t.Errorf("concurrent store operation failed: %v", err)
	}
	configured, err := target.IsConfigured()
	if err != nil || !configured {
		t.Fatalf("imported store is not configured: %v", err)
	}
}

func TestImportDatabasePostSwapFailureRestoresAndReopens(t *testing.T) {
	target := NewUnconfiguredStore(t, filepath.Join(t.TempDir(), "wirehub.db"))
	source := configuredStore(t, filepath.Join(t.TempDir(), "source.db"))
	var snapshot bytes.Buffer
	if err := source.ExportDatabase(&snapshot); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(target.DatabasePath()), "upload.db")
	if err := os.WriteFile(path, snapshot.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	fail := true
	target.SetOpenHookForTest(func() error {
		if fail {
			fail = false
			return errors.New("injected reopen failure")
		}
		return nil
	})
	if err := target.ImportDatabase(path); err == nil {
		t.Fatal("expected post-swap failure")
	}
	configured, err := target.IsConfigured()
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		t.Fatal("failed import replaced the live store")
	}
}

func TestImportDatabaseWALSource(t *testing.T) {
	source := configuredStore(t, filepath.Join(t.TempDir(), "source.db"))
	source.lease.RLock()
	if err := source.db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		source.lease.RUnlock()
		t.Fatal(err)
	}
	if err := source.db.Create(&Peer{Name: "wal-peer", PublicKey: "wal-public", PrivateKey: "wal-private", WGIP: "100.127.0.2", GroupID: 1}).Error; err != nil {
		source.lease.RUnlock()
		t.Fatal(err)
	}
	source.lease.RUnlock()
	target := NewUnconfiguredStore(t, filepath.Join(t.TempDir(), "target.db"))
	path := filepath.Join(filepath.Dir(target.DatabasePath()), "wal-upload.db")
	if err := copyFile(source.DatabasePath(), path); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = copyOptionalFile(source.DatabasePath()+suffix, path+suffix)
	}
	if err := target.ImportDatabase(path); err != nil {
		t.Fatal(err)
	}
	peers, err := target.ListPeers()
	if err != nil || len(peers) != 1 {
		t.Fatalf("WAL import lost peer: %v, %d", err, len(peers))
	}
}

func TestCompositeOperationDoesNotReacquireLeaseBehindWriter(t *testing.T) {
	target := NewUnconfiguredStore(t, filepath.Join(t.TempDir(), "target.db"))
	source := configuredStore(t, filepath.Join(t.TempDir(), "source.db"))
	var snapshot bytes.Buffer
	if err := source.ExportDatabase(&snapshot); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(target.DatabasePath()), "upload.db")
	if err := os.WriteFile(path, snapshot.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	target.SetListMapDetailsHookForTest(func() { close(entered); <-release })
	importDone := make(chan error, 1)
	compositeDone := make(chan error, 1)
	go func() { _, err := target.ListMapDetails(); compositeDone <- err }()
	<-entered
	go func() { importDone <- target.ImportDatabase(path) }()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		if target.lease.TryRLock() {
			target.lease.RUnlock()
		} else {
			break
		}
		runtime.Gosched()
		select {
		case <-deadline.C:
			t.Fatal("maintenance writer was not queued while composite reader was held")
		default:
		}
	}
	close(release)
	select {
	case err := <-compositeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("composite operation blocked behind queued writer")
	}
	if err := <-importDone; err != nil {
		t.Fatal(err)
	}
}

func TestExportDatabaseConcurrentWritesProducesValidSnapshot(t *testing.T) {
	st := configuredStore(t, filepath.Join(t.TempDir(), "wirehub.db"))
	peer := &Peer{Name: "snapshot-peer", PublicKey: "snapshot-public", PrivateKey: "snapshot-private", WGIP: "100.127.0.2", GroupID: 1}
	if err := st.CreatePeer(peer); err != nil {
		t.Fatal(err)
	}
	snapshotStarted := make(chan struct{})
	releaseSnapshot := make(chan struct{})
	st.SetSnapshotHookForTest(func() { close(snapshotStarted); <-releaseSnapshot })
	var snapshot bytes.Buffer
	exportDone := make(chan error, 1)
	go func() { exportDone <- st.ExportDatabase(&snapshot) }()
	<-snapshotStarted
	writerDone := make(chan error, 1)
	go func() { writerDone <- st.UpdatePeerStats(peer.ID, 7, 11, 13) }()
	if err := <-writerDone; err != nil {
		t.Fatal(err)
	}
	close(releaseSnapshot)
	if err := <-exportDone; err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "snapshot.db")
	if err := os.WriteFile(path, snapshot.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWireHubDatabase(path); err != nil {
		t.Fatalf("concurrent snapshot is invalid: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var rx int64
	if err := db.QueryRow("SELECT rx_bytes FROM peers WHERE id = ?", peer.ID).Scan(&rx); err != nil {
		t.Fatal(err)
	}
	if rx != 11 {
		t.Fatalf("snapshot omitted concurrent row update: got rx_bytes=%d", rx)
	}
}

func NewUnconfiguredStore(t *testing.T, path string) *Store {
	t.Helper()
	st, err := New(&config.RuntimeConfig{DatabasePath: path})
	if err != nil {
		t.Fatal(err)
	}
	return st
}
