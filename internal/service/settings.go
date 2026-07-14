package service

import (
	"errors"
	"io"

	"github.com/touken928/wirehub/internal/repo"
)

var (
	ErrInvalidAdminPassword = errors.New("invalid admin password")
	ErrAdminSessionRevoked  = errors.New("token revoked")
)

// SettingsView is the settings page payload.
type SettingsView struct {
	Endpoint        string
	Subnet          string
	AdminUsername   string
	HubIP           string
	DNSIP           string
	DNSSuffix       string
	ListenPort      int
	ServerPublicKey string
	MTU             int
	StatusInterval  int
	UpstreamDNS     []string
}

// GetSettingsView loads settings and primary admin username for the UI.
func (a *App) GetSettingsView() (SettingsView, error) {
	settings, err := a.store.GetSettings()
	if err != nil {
		return SettingsView{}, err
	}
	adminUsername := ""
	if admin, err := a.store.GetPrimaryAdmin(); err == nil {
		adminUsername = admin.Username
	}
	return SettingsView{
		Endpoint:        settings.Endpoint,
		Subnet:          settings.WGSubnet,
		AdminUsername:   adminUsername,
		HubIP:           settings.HubIP,
		DNSIP:           settings.DNSIP,
		DNSSuffix:       settings.DNSSuffix,
		ListenPort:      settings.ListenPort,
		ServerPublicKey: settings.ServerPublicKey,
		MTU:             settings.MTU,
		StatusInterval:  settings.StatusInterval,
		UpstreamDNS:     settings.UpstreamDNSResolvers(),
	}, nil
}

// UpdateSettingsResult reports whether the VPN stack must be restarted.
type UpdateSettingsResult struct {
	RestartRequired bool
}

// UpdateMutableSettings persists MTU, status interval, and upstream DNS; refreshes runtime when needed.
func (a *App) UpdateMutableSettings(mtu, statusInterval int, upstream []string) (UpdateSettingsResult, error) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.updateMutableSettings(mtu, statusInterval, upstream)
}

func (a *App) updateMutableSettings(mtu, statusInterval int, upstream []string) (UpdateSettingsResult, error) {
	settings, err := a.store.GetSettings()
	if err != nil {
		return UpdateSettingsResult{}, err
	}
	oldMTU := settings.MTU
	if err := a.store.UpdateMutableSettings(mtu, statusInterval, upstream); err != nil {
		return UpdateSettingsResult{}, err
	}
	settings, err = a.store.GetSettings()
	if err != nil {
		return UpdateSettingsResult{}, err
	}
	networkReload := settings.MTU != oldMTU
	if err := a.reconcileRuntime("mutable settings update", networkReload); err != nil {
		return UpdateSettingsResult{}, err
	}
	a.Hub.setDNSUpstream(settings.UpstreamDNSResolvers())
	a.Hub.stopStatusPoller()
	a.Hub.startStatusPoller(settings.StatusInterval)
	return UpdateSettingsResult{RestartRequired: networkReload}, nil
}

// SetDNSUpstream updates upstream resolvers on the live DNS server when the stack is running.
func (h *Hub) setDNSUpstream(upstream []string) {
	h.networkMu.RLock()
	nc := h.network
	h.networkMu.RUnlock()
	if nc != nil {
		nc.SetDNSUpstream(upstream)
	}
}

// ChangeAdminPassword verifies the current password and updates it.
func (a *App) ChangeAdminPassword(username, currentPassword, newPassword string) error {
	admin, err := a.store.GetAdminByUsername(username)
	if err != nil {
		return err
	}
	if err := repo.VerifyPassword(admin.PasswordHash, currentPassword); err != nil {
		return ErrInvalidAdminPassword
	}
	return a.store.UpdateAdminPassword(admin.ID, newPassword)
}

// ExportDatabase streams the SQLite file to w.
func (a *App) ExportDatabase(w io.Writer) error {
	return a.store.ExportDatabase(w)
}

// DatabasePath returns the on-disk SQLite path.
func (a *App) DatabasePath() string {
	return a.store.DatabasePath()
}
