package bootstrap

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/touken928/wirehub/internal/config"
)

func TestSetupURLHostBindVariants(t *testing.T) {
	tests := []struct {
		name       string
		bind       string
		port       int
		listenAddr string
		want       string
	}{
		{name: "wildcard IPv4", bind: "0.0.0.0", port: 8443, listenAddr: "0.0.0.0:8443", want: "localhost:8443"},
		{name: "wildcard IPv6", bind: "::", port: 9443, listenAddr: "[::]:9443", want: "localhost:9443"},
		{name: "loopback IPv4", bind: "127.0.0.1", port: 8443, listenAddr: "127.0.0.1:8443", want: "127.0.0.1:8443"},
		{name: "specific IPv4", bind: "192.168.1.20", port: 8080, listenAddr: "192.168.1.20:8080", want: "192.168.1.20:8080"},
		{name: "loopback IPv6", bind: "::1", port: 8443, listenAddr: "[::1]:8443", want: "[::1]:8443"},
		{name: "invalid bind", bind: "hostname", port: 8443, listenAddr: "hostname:8443", want: "hostname:8443"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.RuntimeConfig{Bind: tt.bind, Port: tt.port, ListenAddr: tt.listenAddr}
			if got := setupURLHost(cfg); got != tt.want {
				t.Fatalf("setupURLHost() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetupURLHostCurrentSetupURLBehavior(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.RuntimeConfig
		want string
	}{
		{
			name: "wildcard bind uses localhost and configured port",
			cfg:  config.RuntimeConfig{Bind: "0.0.0.0", Port: 12345, ListenAddr: "0.0.0.0:12345"},
			want: "http://localhost:12345/setup",
		},
		{
			name: "specific bind uses listen address verbatim",
			cfg:  config.RuntimeConfig{Bind: "10.0.0.5", Port: 12345, ListenAddr: "10.0.0.5:12345"},
			want: "http://10.0.0.5:12345/setup",
		},
		{
			name: "IPv6 wildcard uses localhost",
			cfg:  config.RuntimeConfig{Bind: "::", Port: 12345, ListenAddr: "[::]:12345"},
			want: "http://localhost:12345/setup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fmt.Sprintf("http://%s/setup", setupURLHost(&tt.cfg))
			if got != tt.want {
				t.Fatalf("setup URL = %q, want %q", got, tt.want)
			}
		})
	}
}

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
