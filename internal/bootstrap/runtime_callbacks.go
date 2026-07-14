package bootstrap

import (
	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
	"github.com/touken928/wirehub/internal/service"
	"github.com/touken928/wirehub/internal/vpn/runtime"
)

type runtimeCallbacks struct {
	app *service.App
}

func (c runtimeCallbacks) LoadSyncBundle() (domainruntime.SyncBundle, error) {
	return c.app.LoadSyncBundle()
}

func (c runtimeCallbacks) OnStarted(stack *runtime.Stack) {
	c.app.OnStarted(stack)
}

func (c runtimeCallbacks) OnStopped() {
	c.app.OnStopped()
}

var _ runtime.Callbacks = runtimeCallbacks{}
var _ service.Dataplane = (*runtime.Stack)(nil)
