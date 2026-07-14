package service

import (
	"errors"
	"testing"
)

func TestAuthenticateAdminCredentialFailure(t *testing.T) {
	app := testSettingsApp(t)

	if _, err := app.AuthenticateAdmin("admin", "wrong-password"); !errors.Is(err, ErrInvalidAdminPassword) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidAdminPassword)
	}
}

func TestValidateAdminSessionRejectsInvalidatedTokenVersion(t *testing.T) {
	app := testSettingsApp(t)
	session, err := app.AuthenticateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	validated, err := app.ValidateAdminSession(session.ID, session.TokenVersion)
	if err != nil {
		t.Fatalf("current session rejected: %v", err)
	}
	if validated != session {
		t.Fatalf("validated session = %+v, want %+v", validated, session)
	}

	if err := app.store.UpdateAdminPassword(session.ID, "newpassword123"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ValidateAdminSession(session.ID, session.TokenVersion); !errors.Is(err, ErrAdminSessionRevoked) {
		t.Fatalf("stale session error = %v, want %v", err, ErrAdminSessionRevoked)
	}
	current, err := app.store.GetAdminByID(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.ValidateAdminSession(session.ID, current.TokenVersion); err != nil {
		t.Fatalf("current token version rejected: %v", err)
	}
}
