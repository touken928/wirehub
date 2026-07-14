package config

import (
	"flag"
	"io"
	"os"
	"testing"
)

func TestFirstHostIP(t *testing.T) {
	got, err := FirstHostIP("100.127.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if got != "100.127.0.1" {
		t.Fatalf("got %s, want 100.127.0.1", got)
	}
}

func TestParseFlagsCustomArguments(t *testing.T) {
	dir := t.TempDir()
	setArgs(t, "wirehub", "-port", "9000", "-bind", "127.0.0.1", "-data-dir", dir)

	cfg, err := ParseFlags()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9000 {
		t.Fatalf("port = %d, want 9000", cfg.Port)
	}
	if cfg.Bind != "127.0.0.1" {
		t.Fatalf("bind = %q, want 127.0.0.1", cfg.Bind)
	}
	if cfg.DataDir != dir || cfg.ListenAddr != "127.0.0.1:9000" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.DatabasePath != dir+string(os.PathSeparator)+"wirehub.db" {
		t.Fatalf("database path = %q", cfg.DatabasePath)
	}
	if cfg.JWTSecret == "" {
		t.Fatal("expected auto-generated jwt secret")
	}
}

func TestParseFlagsInvalidPort(t *testing.T) {
	setArgs(t, "wirehub", "-port", "0", "-data-dir", t.TempDir())

	cfg, err := ParseFlags()
	if err == nil {
		t.Fatalf("expected error, got config %+v", cfg)
	}
	if err.Error() != "port must be between 1 and 65535" {
		t.Fatalf("error = %q", err)
	}
}

func TestParseFlagsDefaultArguments(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	setArgs(t, "wirehub")

	cfg, err := ParseFlags()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != DefaultPort || cfg.Bind != DefaultBind || cfg.DataDir != DefaultDataDir {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.ListenAddr != "0.0.0.0:8443" || cfg.DatabasePath != "data/wirehub.db" {
		t.Fatalf("unexpected derived defaults: %+v", cfg)
	}
}

func setArgs(t *testing.T, args ...string) {
	t.Helper()
	oldArgs := os.Args
	oldFlags := flag.CommandLine
	os.Args = args
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldFlags
	})
}
