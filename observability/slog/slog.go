package slog

import (
	"context"
	logslog "log/slog"

	cache "github.com/faustbrian/go-cache"
	cacheslog "github.com/faustbrian/go-cache/adapters/slog"
)

// Config selects the logger and level used by an Observer.
type Config struct {
	Logger      *logslog.Logger
	Level       logslog.Level
	IncludeSize bool
}

// Observer preserves the legacy slog adapter type identity.
type Observer struct{ inner *cacheslog.Observer }

// New validates config and constructs a logging observer.
func New(config Config) (*Observer, error) {
	inner, err := cacheslog.New(cacheslog.Config(config))
	if err != nil {
		return nil, err
	}

	return &Observer{inner: inner}, nil
}

// Observe logs one event without key or value data.
func (observer *Observer) Observe(ctx context.Context, event cache.Event) error {
	return observer.inner.Observe(ctx, event)
}
