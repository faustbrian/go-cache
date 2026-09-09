package otel

import (
	"context"

	cache "github.com/faustbrian/go-cache"
	cacheotel "github.com/faustbrian/go-cache/adapters/otel"
	"go.opentelemetry.io/otel/metric"
)

// Observer preserves the legacy OpenTelemetry adapter type identity.
type Observer struct{ inner *cacheotel.Observer }

// New constructs all instruments required by an Observer.
func New(meter metric.Meter) (*Observer, error) {
	inner, err := cacheotel.New(meter)
	if err != nil {
		return nil, err
	}

	return &Observer{inner: inner}, nil
}

// Observe records one validated, low-cardinality cache event.
func (observer *Observer) Observe(ctx context.Context, event cache.Event) error {
	return observer.inner.Observe(ctx, event)
}
