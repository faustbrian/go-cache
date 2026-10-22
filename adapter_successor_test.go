package cache_test

import (
	"errors"
	"testing"

	cache "github.com/faustbrian/go-cache/v4"
	cachememory "github.com/faustbrian/go-cache/v4/adapters/memory"
	cacheotel "github.com/faustbrian/go-cache/v4/adapters/otel"
	"go.opentelemetry.io/otel/metric"
)

type nilMeter struct{ metric.Meter }

func TestAdapterConstructorsRejectTypedNilDependencies(t *testing.T) {
	for name, construct := range map[string]func(cache.Clock, cache.Observer) error{
		"canonical memory": func(clock cache.Clock, observer cache.Observer) error {
			_, err := cachememory.New(cachememory.Config{MaxEntries: 1, MaxBytes: 128, Clock: clock, Observer: observer})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := construct((*nilClock)(nil), nil); !errors.Is(err, cache.ErrInvalidConfig) {
				t.Errorf("typed-nil clock error = %v, want ErrInvalidConfig", err)
			}
			if err := construct(cache.SystemClock{}, (*nilObserver)(nil)); !errors.Is(err, cache.ErrInvalidConfig) {
				t.Errorf("typed-nil observer error = %v, want ErrInvalidConfig", err)
			}
			if err := construct(cache.SystemClock{}, nil); err != nil {
				t.Errorf("absent optional observer error = %v", err)
			}
		})
	}
	for name, construct := range map[string]func(metric.Meter) error{
		"canonical otel": func(meter metric.Meter) error { _, err := cacheotel.New(meter); return err },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("typed-nil meter panicked")
				}
			}()
			if err := construct((*nilMeter)(nil)); err == nil {
				t.Error("typed-nil meter accepted")
			}
		})
	}
}
