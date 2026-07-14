package auth

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/touken928/wirehub/internal/service"
)

type testSession struct {
	password string
	version  int
}

func (s *testSession) AuthenticateAdmin(username, password string) (service.AdminSession, error) {
	if username != "admin" || password != s.password {
		return service.AdminSession{}, errors.New("invalid credentials")
	}
	return service.AdminSession{ID: 1, Username: username, TokenVersion: s.version}, nil
}

func (s *testSession) ValidateAdminSession(id uint, tokenVersion int) (service.AdminSession, error) {
	if id != 1 {
		return service.AdminSession{}, errors.New("admin not found")
	}
	if tokenVersion != s.version {
		return service.AdminSession{}, errors.New("token revoked")
	}
	return service.AdminSession{ID: id, Username: "admin", TokenVersion: s.version}, nil
}

func newTestSession() *testSession { return &testSession{password: "testpass123"} }

func TestLoginAndParse_Success(t *testing.T) {
	session := newTestSession()
	svc := NewService("test-secret", session)

	token, err := svc.Login("admin", "testpass123")
	if err != nil || token == "" {
		t.Fatalf("Login failed: %v", err)
	}
	claims, err := svc.ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken failed: %v", err)
	}
	if claims.Username != "admin" || claims.TokenVersion != 0 {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != "HS256" {
			t.Fatalf("algorithm = %s, want HS256", token.Method.Alg())
		}
		return []byte("test-secret"), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("issued token did not validate: %v", err)
	}
	raw := parsed.Claims.(*Claims)
	if raw.AdminID != 1 || raw.Username != "admin" || raw.TokenVersion != 0 {
		t.Fatalf("unexpected claims: %+v", raw)
	}
	remaining := time.Until(raw.ExpiresAt.Time)
	if math.Abs(remaining.Hours()-24) > 5.0/60.0 {
		t.Fatalf("expiry is %s from now, want approximately 24h", remaining)
	}
}

func TestParseToken_InvalidCredentials(t *testing.T) {
	svc := NewService("test-secret", newTestSession())
	if _, err := svc.Login("admin", "wrongpass"); err == nil {
		t.Fatal("expected error for wrong password")
	}
	if _, err := svc.Login("nonexistent", "testpass123"); err == nil {
		t.Fatal("expected error for nonexistent user")
	}
}

func TestParseToken_WrongSecret(t *testing.T) {
	session := newTestSession()
	svc := NewService("test-secret", session)
	token, err := svc.Login("admin", "testpass123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewService("different-secret", session).ParseToken(token); err == nil {
		t.Fatal("expected error when parsing token with wrong secret")
	}
}

func TestTokenInvalidatedAfterPasswordChange(t *testing.T) {
	session := newTestSession()
	svc := NewService("test-secret", session)
	oldToken, err := svc.Login("admin", "testpass123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ParseToken(oldToken); err != nil {
		t.Fatalf("old token should work before password change: %v", err)
	}
	session.password = "newstrongpass1"
	session.version++
	if _, err := svc.ParseToken(oldToken); err == nil {
		t.Fatal("expected error: old token should be invalid after password change")
	}
	newToken, err := svc.Login("admin", "newstrongpass1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ParseToken(newToken); err != nil {
		t.Fatalf("new token should work after password change: %v", err)
	}
}

func TestParseToken_InvalidTokenString(t *testing.T) {
	svc := NewService("test-secret", newTestSession())
	for _, token := range []string{"not-a-valid-jwt", ""} {
		if _, err := svc.ParseToken(token); err == nil {
			t.Fatalf("expected error for token %q", token)
		}
	}
}
