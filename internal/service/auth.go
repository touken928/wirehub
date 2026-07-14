package service

import "github.com/touken928/wirehub/internal/repo"

// AdminSession is the account state required by an authentication boundary.
// It intentionally excludes password hashes and other persistence details.
type AdminSession struct {
	ID           uint
	Username     string
	TokenVersion int
}

// AuthenticateAdmin verifies an administrator's password and returns session state.
func (a *App) AuthenticateAdmin(username, password string) (AdminSession, error) {
	admin, err := a.store.GetAdminByUsername(username)
	if err != nil {
		return AdminSession{}, err
	}
	if err := repo.VerifyPassword(admin.PasswordHash, password); err != nil {
		return AdminSession{}, ErrInvalidAdminPassword
	}
	return adminSessionFromRepo(admin), nil
}

// VerifyAdminPassword checks credentials when the authenticated account is not needed.
func (a *App) VerifyAdminPassword(username, password string) error {
	_, err := a.AuthenticateAdmin(username, password)
	return err
}

// ValidateAdminSession verifies that a session's token version is current.
func (a *App) ValidateAdminSession(id uint, tokenVersion int) (AdminSession, error) {
	admin, err := a.store.GetAdminByID(id)
	if err != nil {
		return AdminSession{}, err
	}
	if admin.TokenVersion != tokenVersion {
		return AdminSession{}, ErrAdminSessionRevoked
	}
	return adminSessionFromRepo(admin), nil
}

func adminSessionFromRepo(admin *repo.Admin) AdminSession {
	return AdminSession{ID: admin.ID, Username: admin.Username, TokenVersion: admin.TokenVersion}
}
