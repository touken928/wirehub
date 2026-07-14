package service

import (
	"sync"

	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
	"github.com/touken928/wirehub/internal/repo"
)

// App is the control-plane application root (persistence + network hub).
type App struct {
	store        *repo.Store
	keyGenerator KeyGenerator
	Hub          *Hub
	Status       *StatusService
	controlMu    sync.Mutex
	setupTokenMu sync.RWMutex
	setupToken   string
}

// SetSetupToken installs the first-run token owned by the application lifecycle.
func (a *App) SetSetupToken(token string) {
	a.setupTokenMu.Lock()
	a.setupToken = token
	a.setupTokenMu.Unlock()
}

func (a *App) SetupToken() string {
	a.setupTokenMu.RLock()
	defer a.setupTokenMu.RUnlock()
	return a.setupToken
}

func (a *App) setupTokenValid(token string) bool {
	a.setupTokenMu.RLock()
	defer a.setupTokenMu.RUnlock()
	return token != "" && token == a.setupToken
}

// NewApp wires the hub and status service to this application instance.
func NewApp(st *repo.Store, keyGenerator KeyGenerator) *App {
	if keyGenerator == nil {
		panic("service.NewApp requires a non-nil key generator")
	}
	a := &App{store: st, keyGenerator: keyGenerator}
	a.Hub = newHub(a)
	a.Status = newStatusService(a)
	a.Hub.setStatusPublisher(a.Status)
	return a
}

// LoadSyncBundle implements runtime.Callbacks.
func (a *App) LoadSyncBundle() (domainruntime.SyncBundle, error) {
	return a.loadSyncBundle()
}

// OnStarted is the lifecycle bridge from vpn/runtime into service-owned interfaces.
func (a *App) OnStarted(dp Dataplane) {
	a.Hub.onStarted(dp)
}

// OnStopped implements runtime.Callbacks.
func (a *App) OnStopped() {
	a.Hub.onStopped()
}
