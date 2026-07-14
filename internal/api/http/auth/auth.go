package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/touken928/wirehub/internal/service"
)

// Session is the application boundary used by HTTP authentication. It keeps
// persistence and password hashing outside the HTTP layer.
type Session interface {
	AuthenticateAdmin(username, password string) (service.AdminSession, error)
	ValidateAdminSession(id uint, tokenVersion int) (service.AdminSession, error)
}

// Claims is the JWT payload for an admin session.
type Claims struct {
	AdminID      uint   `json:"admin_id"`
	Username     string `json:"username"`
	TokenVersion int    `json:"token_version"`
	jwt.RegisteredClaims
}

// Service issues and validates admin JWTs.
type Service struct {
	secret  string
	session Session
}

// NewService wires JWT signing to the application session boundary.
func NewService(secret string, session Session) *Service {
	return &Service{secret: secret, session: session}
}

// Login validates credentials and returns a bearer token.
func (s *Service) Login(username, password string) (string, error) {
	admin, err := s.session.AuthenticateAdmin(username, password)
	if err != nil {
		return "", errors.New("invalid credentials")
	}
	return s.issueToken(admin)
}

func (s *Service) issueToken(admin service.AdminSession) (string, error) {
	claims := Claims{
		AdminID:      admin.ID,
		Username:     admin.Username,
		TokenVersion: admin.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.secret))
}

// ParseToken validates a bearer token and returns its claims.
// It checks the embedded TokenVersion against the stored admin to revoke tokens
// issued before a password change.
func (s *Service) ParseToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(s.secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	_, err = s.session.ValidateAdminSession(claims.AdminID, claims.TokenVersion)
	if err != nil {
		if errors.Is(err, service.ErrAdminSessionRevoked) {
			return nil, errors.New("token revoked")
		}
		return nil, errors.New("admin not found")
	}
	return claims, nil
}
