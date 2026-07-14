package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func TestMiddlewareCharacterization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	session := newTestSession()
	svc := NewService("test-secret", session)
	valid, err := svc.Login("admin", "testpass123")
	if err != nil {
		t.Fatal(err)
	}
	session.password = "newstrongpass1"
	session.version++
	// Keep the expired token on the current token version so expiry is tested
	// independently from password-change revocation.
	expired := signedTestToken(t, "test-secret", Claims{AdminID: 1, Username: "admin", TokenVersion: 1, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}})
	current, err := svc.Login("admin", "newstrongpass1")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, header string
		code         int
		body         string
		wantNext     bool
		wantID       uint
		wantUser     string
	}{
		{name: "missing", code: http.StatusUnauthorized, body: `{"error":"missing authorization"}`},
		{name: "valid", header: "Bearer " + current, code: http.StatusOK, wantNext: true, wantID: 1, wantUser: "admin"},
		{name: "expired", header: "Bearer " + expired, code: http.StatusUnauthorized, body: `{"error":"invalid token"}`},
		{name: "wrong version", header: "Bearer " + valid, code: http.StatusUnauthorized, body: `{"error":"invalid token"}`},
		{name: "wrong scheme", header: "Basic " + valid, code: http.StatusUnauthorized, body: `{"error":"invalid authorization header"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			var gotID any
			var gotUser any
			r.Use(Middleware(svc))
			r.GET("/protected", func(c *gin.Context) {
				gotID, _ = c.Get("admin_id")
				gotUser, _ = c.Get("username")
				c.Status(http.StatusOK)
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			r.ServeHTTP(w, req)
			if w.Code != tt.code {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, tt.code, w.Body.String())
			}
			if tt.body != "" && w.Body.String() != tt.body {
				t.Fatalf("body = %q, want %q", w.Body.String(), tt.body)
			}
			if tt.wantNext {
				if gotID != tt.wantID {
					t.Fatalf("admin_id = %#v, want %d", gotID, tt.wantID)
				}
				if gotUser != tt.wantUser {
					t.Fatalf("username = %#v, want %q", gotUser, tt.wantUser)
				}
			}
			if (gotID != nil) != tt.wantNext {
				t.Fatalf("next = %v, want %v", gotID != nil, tt.wantNext)
			}
		})
	}
}

func signedTestToken(t *testing.T, secret string, claims Claims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}
