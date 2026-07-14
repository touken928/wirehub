package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateJWTSecretCreatesAndReuses(t *testing.T) {
	dir := t.TempDir()

	first, err := loadOrCreateJWTSecret(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" {
		t.Fatal("expected generated secret")
	}
	data, err := os.ReadFile(filepath.Join(dir, ".jwt_secret"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != first {
		t.Fatalf("file contents = %q, want %q", data, first)
	}

	second, err := loadOrCreateJWTSecret(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("reused secret = %q, want %q", second, first)
	}
}

func TestLoadOrCreateJWTSecretReplacesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".jwt_secret")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	secret, err := loadOrCreateJWTSecret(dir)
	if err != nil {
		t.Fatal(err)
	}
	if secret == "" {
		t.Fatal("expected generated secret")
	}
}

func TestLoadOrCreateJWTSecretReusesMalformedNonEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".jwt_secret")
	const malformed = "not-a-valid-base64-secret"
	if err := os.WriteFile(path, []byte(malformed), 0o600); err != nil {
		t.Fatal(err)
	}

	secret, err := loadOrCreateJWTSecret(dir)
	if err != nil {
		t.Fatal(err)
	}
	if secret != malformed {
		t.Fatalf("secret = %q, want %q", secret, malformed)
	}
}
