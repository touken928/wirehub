package service

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/touken928/wirehub/internal/config"
	"github.com/touken928/wirehub/internal/repo"
)

// SetupDefaults are shown on the setup page before configuration.
type SetupDefaults struct {
	Subnet         string
	AdminUsername  string
	ListenPort     int
	MTU            int
	StatusInterval int
	UpstreamDNS    []string
}

// SetupStatus reports whether the hub database is initialized.
func (a *App) SetupStatus() (configured bool, defaults SetupDefaults, err error) {
	configured, err = a.store.IsConfigured()
	if err != nil {
		return false, SetupDefaults{}, err
	}
	return configured, SetupDefaults{
		Subnet:         config.DefaultSubnet,
		AdminUsername:  config.DefaultAdminUsername,
		ListenPort:     config.DefaultEndpointPort,
		MTU:            config.DefaultMTU,
		StatusInterval: config.DefaultStatusInterval,
		UpstreamDNS:    append([]string(nil), config.DefaultUpstreamDNS...),
	}, nil
}

// SetupInput is the first-time hub configuration payload.
type SetupInput struct {
	Endpoint       string
	Subnet         string
	AdminUsername  string
	AdminPassword  string
	ListenPort     int
	MTU            int
	StatusInterval int
	UpstreamDNS    []string
}

// Setup initializes the hub database and starts the network stack when available.
func (a *App) Setup(in SetupInput) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.setup(in, "", false)
}

// SetupWithToken performs the setup transition after rechecking token and state
// while holding the application lifecycle lock.
func (a *App) SetupWithToken(in SetupInput, token string) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.setup(in, token, true)
}

func (a *App) setup(in SetupInput, token string, requireToken bool) error {
	configured, err := a.store.IsConfigured()
	if err != nil {
		return err
	}
	if configured {
		return ErrAlreadyConfigured
	}
	if requireToken && !a.setupTokenValid(token) {
		return ErrSetupTokenRequired
	}
	listenPort := in.ListenPort
	if listenPort == 0 {
		listenPort = config.DefaultEndpointPort
	}
	priv, pub, err := a.keyGenerator()
	if err != nil {
		return err
	}
	if err := a.store.Setup(repo.SetupInput{
		Endpoint:         in.Endpoint,
		Subnet:           in.Subnet,
		AdminUsername:    in.AdminUsername,
		AdminPassword:    in.AdminPassword,
		MTU:              in.MTU,
		StatusInterval:   in.StatusInterval,
		ListenPort:       listenPort,
		ServerPrivateKey: priv,
		ServerPublicKey:  pub,
		UpstreamDNS:      in.UpstreamDNS,
	}); err != nil {
		return err
	}
	return a.startNetworkAfterSetup()
}

func (a *App) startNetworkAfterSetup() error {
	net := a.Hub.networkRuntime()
	if net == nil {
		return a.stopAfterRuntimeFailure("setup", ErrNetworkUnavailable)
	}
	bundle, err := a.LoadSyncBundle()
	if err != nil {
		return a.stopAfterRuntimeFailure("setup", err)
	}
	if err := net.Start(bundle); err != nil {
		return a.stopAfterRuntimeFailure("setup", err)
	}
	return nil
}

// ImportDatabase replaces the SQLite file before the hub is configured.
func (a *App) ImportDatabase(tmpPath string) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.importDatabase(tmpPath, "", false)
}

// ImportDatabaseWithToken performs the import transition after rechecking the
// setup token and configured state under the same lifecycle lock as setup/reset.
func (a *App) ImportDatabaseWithToken(tmpPath, token string) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.importDatabase(tmpPath, token, true)
}

func (a *App) importDatabase(tmpPath, token string, requireToken bool) error {
	configured, err := a.store.IsConfigured()
	if err != nil {
		return err
	}
	if configured {
		return ErrImportWhenConfigured
	}
	if requireToken && !a.setupTokenValid(token) {
		return ErrSetupTokenRequired
	}
	if err := a.store.ImportDatabase(tmpPath); err != nil {
		return err
	}
	net := a.Hub.networkRuntime()
	if net == nil {
		return nil
	}
	bundle, err := a.LoadSyncBundle()
	if err != nil {
		return a.stopAfterRuntimeFailure("database import", err)
	}
	if err := net.Start(bundle); err != nil {
		return a.stopAfterRuntimeFailure("database import", err)
	}
	return nil
}

// PrepareDBUploadDir ensures the data directory exists for setup import.
func (a *App) PrepareDBUploadDir() (dataDir string, err error) {
	dataDir = filepath.Dir(a.store.DatabasePath())
	err = os.MkdirAll(dataDir, 0o755)
	return dataDir, err
}

// Reset stops the network stack and clears all hub data after password verification.
func (a *App) Reset() error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.reset()
}

// ResetWithSetupToken atomically resets the hub and publishes the new token
// only after the reset succeeds. The caller updates its delivery-layer copy
// after this method returns.
func (a *App) ResetWithSetupToken(newToken string) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	if err := a.reset(); err != nil {
		return err
	}
	a.SetSetupToken(newToken)
	return nil
}

// ResetWithAdminPassword keeps credential verification and the destructive
// lifecycle transition in one serialized operation.
func (a *App) ResetWithAdminPassword(username, password, newToken string) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	if err := a.VerifyAdminPassword(username, password); err != nil {
		return err
	}
	if err := a.reset(); err != nil {
		return err
	}
	a.SetSetupToken(newToken)
	return nil
}

func (a *App) reset() error {
	net := a.Hub.networkRuntime()
	if net == nil {
		return ErrNetworkUnavailable
	}
	if err := net.Stop(); err != nil {
		return err
	}
	return a.store.ResetAll()
}

// IsConfigured reports whether setup has completed.
func (a *App) IsConfigured() (bool, error) {
	return a.store.IsConfigured()
}

var (
	ErrAlreadyConfigured    = errors.New("already configured")
	ErrImportWhenConfigured = errors.New("hub is already configured; reset before importing a database")
	ErrSetupTokenRequired   = errors.New("setup token required")
)
