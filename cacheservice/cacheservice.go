// Package cacheservice is the legacy cache service-lifecycle adapter.
//
// Deprecated: use github.com/faustbrian/go-cache/adapters/service. This
// package remains supported for the longer of 180 days after successor
// availability and two subsequently published stable root-module minor
// releases.
package cacheservice

import (
	"context"
	"errors"
	"fmt"

	canonical "github.com/faustbrian/go-cache/adapters/service"
	"github.com/faustbrian/go-service"
)

var (
	// ErrInvalidOptions identifies invalid adapter construction.
	ErrInvalidOptions = canonical.ErrInvalidOptions
	// ErrUnavailable identifies a resource that has not started or is stopping.
	ErrUnavailable = canonical.ErrUnavailable
)

// Startup performs optional resource validation before startup succeeds.
type Startup[R any] func(context.Context, R) error

// Check evaluates whether a resource can accept new work.
type Check[R any] func(context.Context, R) error

// Shutdown releases an explicitly transferred resource.
type Shutdown[R any] func(context.Context, R) error

// Options configure one cache lifecycle adapter.
type Options[R any] struct {
	Name      string
	Resource  R
	Startup   Startup[R]
	Readiness Check[R]
	Shutdown  Shutdown[R]
}

// OptionsError identifies one rejected option.
type OptionsError struct {
	Field  string
	Reason string
}

// Error returns a secret-safe construction diagnostic.
func (err *OptionsError) Error() string {
	return fmt.Sprintf("%s: %s: %v", err.Field, err.Reason, ErrInvalidOptions)
}

// Unwrap exposes the stable option classification.
func (err *OptionsError) Unwrap() error { return ErrInvalidOptions }

// StartupError preserves validation and cleanup failures without formatting
// either potentially sensitive cause.
type StartupError struct {
	Validation error
	Cleanup    error
}

// Error returns a secret-safe startup diagnostic.
func (err *StartupError) Error() string {
	if err.Cleanup != nil {
		return "cache service startup validation and cleanup failed"
	}

	return "cache service startup validation failed"
}

// Unwrap preserves both causes for errors.Is and errors.As.
func (err *StartupError) Unwrap() []error {
	causes := []error{err.Validation}
	if err.Cleanup != nil {
		causes = append(causes, err.Cleanup)
	}

	return causes
}

// Adapter preserves the legacy service adapter type identity.
type Adapter[R any] struct{ inner *canonical.Adapter[R] }

// New validates and constructs an inert adapter.
func New[R any](options Options[R]) (*Adapter[R], error) {
	inner, err := canonical.New(canonical.Options[R]{
		Name:      options.Name,
		Resource:  options.Resource,
		Startup:   canonical.Startup[R](options.Startup),
		Readiness: canonical.Check[R](options.Readiness),
		Shutdown:  canonical.Shutdown[R](options.Shutdown),
	})
	if err != nil {
		return nil, translateOptionsError(err)
	}

	return &Adapter[R]{inner: inner}, nil
}

// Resource returns the exact caller-provided concrete resource.
func (adapter *Adapter[R]) Resource() R { return adapter.inner.Resource() }

// Readiness returns the configured opt-in readiness check.
func (adapter *Adapter[R]) Readiness() (service.ReadinessCheck, bool) {
	return adapter.inner.Readiness()
}

// Component returns the ordered service lifecycle component.
func (adapter *Adapter[R]) Component() service.Component {
	component := adapter.inner.Component()
	start := component.Start
	component.Start = func(ctx context.Context) error {
		return translateStartupError(start(ctx))
	}

	return component
}

func translateOptionsError(err error) error {
	var canonicalError *canonical.OptionsError
	if !errors.As(err, &canonicalError) {
		return err
	}

	return &OptionsError{Field: canonicalError.Field, Reason: canonicalError.Reason}
}

func translateStartupError(err error) error {
	var canonicalError *canonical.StartupError
	if !errors.As(err, &canonicalError) {
		return err
	}

	return &StartupError{
		Validation: canonicalError.Validation,
		Cleanup:    canonicalError.Cleanup,
	}
}
