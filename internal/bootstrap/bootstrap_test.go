package bootstrap

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAccessLogRedactsSensitiveQueryValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	r := gin.New()
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{Output: &logs, Formatter: accessLogFormatter}))
	r.GET("/", func(c *gin.Context) { c.Status(200) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/?keep=visible&setup_token=setup-secret&token=jwt-secret", nil)
	r.ServeHTTP(w, req)

	output := logs.String()
	for _, secret := range []string{"setup-secret", "jwt-secret"} {
		if strings.Contains(output, secret) {
			t.Fatalf("access log contains sensitive value %q: %s", secret, output)
		}
	}
	if !strings.Contains(output, "keep=visible") || !strings.Contains(output, "setup_token=REDACTED") || !strings.Contains(output, "token=REDACTED") {
		t.Fatalf("access log did not preserve expected request details: %s", output)
	}
}

func TestRecoveryRedactsSensitiveQueryValues(t *testing.T) {
	gin.SetMode(gin.DebugMode)
	var logs bytes.Buffer
	r := gin.New()
	r.Use(gin.RecoveryWithWriter(&recoveryLogWriter{inner: &logs}))
	r.GET("/", func(c *gin.Context) { panic(http.ErrAbortHandler) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/?setup_token=setup-secret&token=jwt-secret", nil)
	r.ServeHTTP(w, req)

	output := logs.String()
	for _, secret := range []string{"setup-secret", "jwt-secret"} {
		if strings.Contains(output, secret) {
			t.Fatalf("recovery log contains sensitive value %q: %s", secret, output)
		}
	}
	if !strings.Contains(output, "setup_token=REDACTED") || !strings.Contains(output, "token=REDACTED") {
		t.Fatalf("recovery log did not contain redacted request path: %s", output)
	}
}

func TestRecoveryPreservesNormalPanicResponse(t *testing.T) {
	gin.SetMode(gin.DebugMode)
	var logs bytes.Buffer
	r := gin.New()
	r.Use(gin.RecoveryWithWriter(&recoveryLogWriter{inner: &logs}))
	r.GET("/", func(c *gin.Context) { panic("boom") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/?token=jwt-secret", nil)
	r.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Fatalf("recovered panic status = %d, want 500", w.Code)
	}
	if strings.Contains(logs.String(), "jwt-secret") {
		t.Fatalf("normal recovery log contains sensitive value: %s", logs.String())
	}
}
