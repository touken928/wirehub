package service

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/touken928/wirehub/internal/config"
	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
	"github.com/touken928/wirehub/internal/repo"
)

type transitionBarrierRuntime struct {
	mu      sync.Mutex
	block   bool
	entered chan struct{}
	release chan struct{}
}

func (r *transitionBarrierRuntime) Start(_ domainruntime.SyncBundle) error {
	r.mu.Lock()
	block := r.block
	if block {
		r.block = false
	}
	r.mu.Unlock()
	if block {
		close(r.entered)
		<-r.release
	}
	return nil
}
func (r *transitionBarrierRuntime) Stop() error             { return nil }
func (r *transitionBarrierRuntime) ReloadSettings() error   { return nil }
func (r *transitionBarrierRuntime) SyncPortForwards() error { return nil }
func (r *transitionBarrierRuntime) SyncMaps() error         { return nil }
func (r *transitionBarrierRuntime) HubListenPort() int      { return 0 }
func (r *transitionBarrierRuntime) SetDNSUpstream([]string) {}

func transitionSetupInput() SetupInput {
	return SetupInput{
		Endpoint: "example.com", Subnet: "100.127.0.0/24",
		AdminUsername: "admin", AdminPassword: "password123",
		ListenPort: 8443, MTU: 1420, StatusInterval: 1,
	}
}

func TestLifecycleTransitionRevalidatesTokenAfterReset(t *testing.T) {
	targetStore, err := repo.New(&config.RuntimeConfig{DatabasePath: filepath.Join(t.TempDir(), "target.db")})
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(targetStore)
	app.SetSetupToken("old-token")
	runtime := &transitionBarrierRuntime{block: true, entered: make(chan struct{}), release: make(chan struct{})}
	app.Hub.SetNetworkRuntime(runtime)

	setupDone := make(chan error, 1)
	go func() { setupDone <- app.SetupWithToken(transitionSetupInput(), "old-token") }()
	<-runtime.entered
	resetDone := make(chan error, 1)
	go func() { resetDone <- app.ResetWithAdminPassword("admin", "password123", "new-token") }()
	select {
	case err := <-resetDone:
		t.Fatalf("reset bypassed in-flight setup: %v", err)
	default:
	}
	close(runtime.release)
	if err := <-setupDone; err != nil {
		t.Fatal(err)
	}
	if err := <-resetDone; err != nil {
		t.Fatal(err)
	}

	if err := app.SetupWithToken(transitionSetupInput(), "old-token"); !errors.Is(err, ErrSetupTokenRequired) {
		t.Fatalf("old setup token error = %v", err)
	}
	if err := app.SetupWithToken(transitionSetupInput(), "new-token"); err != nil {
		t.Fatalf("new setup token rejected: %v", err)
	}

	// Reset again, then exercise the same final token check on import.
	if err := app.ResetWithAdminPassword("admin", "password123", "import-token"); err != nil {
		t.Fatal(err)
	}
	sourceStore, err := repo.New(&config.RuntimeConfig{DatabasePath: filepath.Join(t.TempDir(), "source.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.Setup(repo.SetupInput{
		Endpoint: "source.example.com", Subnet: "100.127.0.0/24", AdminUsername: "admin",
		AdminPassword: "password123", ListenPort: 8443, MTU: 1420, StatusInterval: 1,
		ServerPrivateKey: "private", ServerPublicKey: "public",
	}); err != nil {
		t.Fatal(err)
	}
	var snapshot bytes.Buffer
	if err := sourceStore.ExportDatabase(&snapshot); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "source.db")
	if err := os.WriteFile(path, snapshot.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.ImportDatabaseWithToken(path, "old-token"); !errors.Is(err, ErrSetupTokenRequired) {
		t.Fatalf("old import token error = %v", err)
	}
	if err := app.ImportDatabaseWithToken(path, "import-token"); err != nil {
		t.Fatalf("new import token rejected: %v", err)
	}
}
