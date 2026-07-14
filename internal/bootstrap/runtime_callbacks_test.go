package bootstrap

import (
	"reflect"
	"testing"

	"github.com/touken928/wirehub/internal/config"
	"github.com/touken928/wirehub/internal/repo"
	"github.com/touken928/wirehub/internal/service"
)

func TestRuntimeCallbacksLoadSyncBundleDelegates(t *testing.T) {
	st, err := repo.New(&config.RuntimeConfig{DatabasePath: t.TempDir() + "/wirehub.db"})
	if err != nil {
		t.Fatalf("repo.New() error = %v", err)
	}
	app := service.NewApp(st, func() (string, string, error) { return "", "", nil })
	callbacks := runtimeCallbacks{app: app}

	want, wantErr := app.LoadSyncBundle()
	got, gotErr := callbacks.LoadSyncBundle()
	if !reflect.DeepEqual(got, want) || (gotErr == nil) != (wantErr == nil) {
		t.Fatalf("LoadSyncBundle() = %#v, %v; want %#v, %v", got, gotErr, want, wantErr)
	}
}

func TestRuntimeCallbacksOnStoppedDelegates(t *testing.T) {
	st, err := repo.New(&config.RuntimeConfig{DatabasePath: t.TempDir() + "/wirehub.db"})
	if err != nil {
		t.Fatalf("repo.New() error = %v", err)
	}
	app := service.NewApp(st, func() (string, string, error) { return "", "", nil })
	callbacks := runtimeCallbacks{app: app}

	callbacks.OnStopped()
}
