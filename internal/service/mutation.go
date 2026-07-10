package service

import (
	"errors"
	"fmt"

	domainruntime "github.com/touken928/wirehub/internal/domain/runtime"
)

type RuntimeState string

const (
	RuntimeStateStopped = RuntimeState("stopped")
	RuntimeStateUnknown = RuntimeState("unknown")
)

// RuntimeMutationError means the requested database mutation committed, but
// the live runtime could not be brought to the requested state. The runtime
// is stopped before this error is returned.
type RuntimeMutationError struct {
	Operation string
	Cause     error
	State     RuntimeState
}

func (e *RuntimeMutationError) Error() string {
	return fmt.Sprintf("%s persisted but runtime is %s: %v", e.Operation, e.State, e.Cause)
}

func (e *RuntimeMutationError) Unwrap() error { return e.Cause }

// RuntimeRecoveryError means the runtime stop itself failed, so its state is
// unknown. It is intentionally distinct from RuntimeMutationError, which only
// claims the runtime is stopped after a confirmed successful stop.
type RuntimeRecoveryError struct {
	Operation string
	Cause     error
}

func (e *RuntimeRecoveryError) Error() string {
	return fmt.Sprintf("%s persisted but runtime state is unknown: %v", e.Operation, e.Cause)
}

func (e *RuntimeRecoveryError) Unwrap() error { return e.Cause }

// RuntimeStoppedError means persistence succeeded and the runtime was
// stopped, but no restart was attempted because the desired bundle could not
// be built or startup failed before reconciliation could begin.
type RuntimeStoppedError struct {
	Operation string
	Cause     error
}

func (e *RuntimeStoppedError) Error() string {
	return fmt.Sprintf("%s persisted but runtime is stopped: %v", e.Operation, e.Cause)
}

func (e *RuntimeStoppedError) Unwrap() error { return e.Cause }

func (a *App) reconcileRuntime(operation string, restart bool) error {
	net := a.Hub.NetworkRuntime()
	if net == nil {
		return nil
	}
	bundle, err := a.LoadSyncBundle()
	if err != nil {
		return a.stopAfterRuntimeFailure(operation, err)
	}
	if restart {
		if err := net.Stop(); err != nil {
			return &RuntimeRecoveryError{Operation: operation, Cause: err}
		}
		a.Hub.onStopped()
		if err := net.Start(bundle); err != nil {
			if stopErr := net.Stop(); stopErr != nil {
				return &RuntimeRecoveryError{Operation: operation, Cause: errors.Join(err, stopErr)}
			}
			a.Hub.onStopped()
			return &RuntimeMutationError{Operation: operation, Cause: err, State: RuntimeStateStopped}
		}
		return nil
	}
	if err := a.Hub.syncRuntimeBundle(bundle); err == nil {
		return nil
	} else {
		return a.restartAfterRuntimeFailure(operation, bundle, err)
	}
}

func (a *App) restartAfterRuntimeFailure(operation string, bundle domainruntime.SyncBundle, syncErr error) error {
	net := a.Hub.NetworkRuntime()
	if err := net.Stop(); err != nil {
		return &RuntimeRecoveryError{Operation: operation, Cause: errors.Join(syncErr, err)}
	}
	a.Hub.onStopped()
	if err := net.Start(bundle); err != nil {
		if stopErr := net.Stop(); stopErr != nil {
			return &RuntimeRecoveryError{Operation: operation, Cause: errors.Join(syncErr, err, stopErr)}
		}
		a.Hub.onStopped()
		return &RuntimeMutationError{Operation: operation, Cause: errors.Join(syncErr, err), State: RuntimeStateStopped}
	}
	return nil
}

func (a *App) stopAfterRuntimeFailure(operation string, cause error) error {
	net := a.Hub.NetworkRuntime()
	if net == nil {
		return &RuntimeRecoveryError{Operation: operation, Cause: cause}
	}
	if err := net.Stop(); err != nil {
		return &RuntimeRecoveryError{Operation: operation, Cause: errors.Join(cause, err)}
	}
	a.Hub.onStopped()
	return &RuntimeStoppedError{Operation: operation, Cause: cause}
}

func IsRuntimeMutationError(err error) bool {
	var target *RuntimeMutationError
	return errors.As(err, &target)
}

func IsRuntimeFailure(err error) bool {
	var recovery *RuntimeRecoveryError
	var stopped *RuntimeStoppedError
	return IsRuntimeMutationError(err) || errors.As(err, &recovery) || errors.As(err, &stopped)
}
