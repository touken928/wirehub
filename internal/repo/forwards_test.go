package repo

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/touken928/wirehub/internal/config"
)

func TestPortForwardNotFoundUsesRepositoryError(t *testing.T) {
	store, err := New(&config.RuntimeConfig{DatabasePath: filepath.Join(t.TempDir(), "wirehub.db")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetPortForward(99); !errors.Is(err, ErrPortForwardNotFound) {
		t.Fatalf("GetPortForward error = %v, want %v", err, ErrPortForwardNotFound)
	}
	if _, err := store.UpdatePortForward(99, 80, PortForwardInput{ListenPort: 9000, Protocol: "tcp", TargetHost: "10.0.0.2", TargetPort: 80}); !errors.Is(err, ErrPortForwardNotFound) {
		t.Fatalf("UpdatePortForward error = %v, want %v", err, ErrPortForwardNotFound)
	}
	if err := store.DeletePortForward(99); err != nil {
		t.Fatalf("DeletePortForward absent ID = %v, want nil", err)
	}
}

func TestPortForwardOccupiedListenPortUsesTypedError(t *testing.T) {
	store, err := New(&config.RuntimeConfig{DatabasePath: filepath.Join(t.TempDir(), "wirehub.db")})
	if err != nil {
		t.Fatal(err)
	}
	input := PortForwardInput{ListenPort: 19090, Protocol: "tcp", TargetHost: "10.0.0.2", TargetPort: 80}
	if _, err := store.CreatePortForward(80, input); err != nil {
		t.Fatal(err)
	}
	_, err = store.CreatePortForward(80, PortForwardInput{ListenPort: 19090, Protocol: "udp", TargetHost: "10.0.0.3", TargetPort: 80})
	var portErr *PortForwardListenPortError
	if !errors.As(err, &portErr) || !errors.Is(err, ErrPortForwardListenPortUsed) {
		t.Fatalf("occupied port error = %v, want typed sentinel error", err)
	}
}
