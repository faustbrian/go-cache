//lint:file-ignore SA1019 Compatibility coverage requires the legacy adapter.

package cache_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"testing"
	"time"

	cache "github.com/faustbrian/go-cache/v3"
	cachememory "github.com/faustbrian/go-cache/v3/adapters/memory"
	canonical "github.com/faustbrian/go-cache/v3/adapters/service"
	legacy "github.com/faustbrian/go-cache/v3/cacheservice" //nolint:staticcheck // Preserve the supported legacy facade.
	"github.com/faustbrian/go-correlation"
	"github.com/faustbrian/go-service"
)

type serviceResource struct {
	closed  bool
	closes  int
	payload string
}

type cacheLifecycle interface {
	Resource() *serviceResource
	Component() service.Component
	Readiness() (service.ReadinessCheck, bool)
}

type lifecycleOptions struct {
	resource  *serviceResource
	startup   func(context.Context, *serviceResource) error
	readiness func(context.Context, *serviceResource) error
	shutdown  func(context.Context, *serviceResource) error
}

var cacheLifecycleFactories = map[string]func(lifecycleOptions) (cacheLifecycle, error){
	"canonical": func(o lifecycleOptions) (cacheLifecycle, error) {
		return canonical.New(canonical.Options[*serviceResource]{
			Name: "cache", Resource: o.resource, Startup: o.startup,
			Readiness: o.readiness, Shutdown: o.shutdown,
		})
	},
	"legacy": func(o lifecycleOptions) (cacheLifecycle, error) {
		return legacy.New(legacy.Options[*serviceResource]{
			Name: "cache", Resource: o.resource, Startup: o.startup,
			Readiness: o.readiness, Shutdown: o.shutdown,
		})
	},
}

type serviceContextKey struct{}

func TestServiceAdapterExecutesOneShotWithDefaultCorrelation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), serviceContextKey{}, "caller"), 3*time.Second)
	defer cancel()
	backend, err := cachememory.New(cachememory.Config{MaxEntries: 1, MaxBytes: 128, Clock: cache.SystemClock{}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := backend.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	closes := 0
	adapter, err := canonical.New(canonical.Options[*cachememory.Backend]{
		Name: "cache", Resource: backend,
		Startup: func(ctx context.Context, resource *cachememory.Backend) error {
			now := time.Now()
			written, err := resource.Set(ctx, "key", cache.Record{Payload: []byte("cached-value"), ExpiresAt: now.Add(time.Minute), StaleAt: now.Add(2 * time.Minute)}, cache.Unconditional)
			if err != nil {
				return err
			}
			if !written {
				return errors.New("cache write rejected")
			}
			return nil
		},
		Shutdown: func(_ context.Context, resource *cachememory.Backend) error {
			closes++
			return resource.Close()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	uuidV4 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	command := service.CommandFor(service.CommandSpec[struct{}]{
		Name: "read-cache", Kind: service.CommandKindOneShot,
		Load: func(context.Context, service.Invocation) (struct{}, error) { return struct{}{}, nil },
		Build: func(context.Context, service.BuildContext, struct{}) (service.Plan, error) {
			return service.Plan{
				Components: []service.Component{adapter.Component()},
				Tasks: []service.Task{{Name: "read-cache", Run: func(ctx context.Context) error {
					values, ok := correlation.FromContext(ctx)
					if !ok || !uuidV4.MatchString(string(values.CorrelationID)) || !uuidV4.MatchString(string(values.RequestID)) || ctx.Value(serviceContextKey{}) != "caller" {
						return errors.New("default identifiers or caller context missing")
					}
					record, found, err := adapter.Resource().Get(ctx, "key")
					if err != nil {
						return err
					}
					if !found {
						return errors.New("started cache value missing")
					}
					_, err = fmt.Fprintln(&stdout, string(record.Payload))
					return err
				}}},
			}, nil
		},
	})
	exit := service.Execute(ctx, service.Definition{
		Identity: service.Identity{Name: "cache-consumer"},
		Commands: service.Commands{Custom: []service.Command{command}},
	}, service.Invocation{Args: []string{"read-cache"}, Stdout: &stdout, Stderr: &stderr})
	if exit != 0 || stdout.String() != "cached-value\n" || stderr.Len() != 0 || closes != 1 {
		t.Fatalf("Execute = %d, stdout = %q, stderr = %q, closes = %d", exit, stdout.String(), stderr.String(), closes)
	}
}

func TestServiceAdapterPreservesCallbackContextAndReadinessRecovery(t *testing.T) {
	for name, factory := range cacheLifecycleFactories {
		t.Run(name, func(t *testing.T) {
			parent, cancel := context.WithTimeout(context.WithValue(context.Background(), serviceContextKey{}, "caller"), time.Second)
			defer cancel()
			ctx, cancelCause := context.WithCancelCause(parent)
			cause := errors.New("caller cancellation")
			defer cancelCause(cause)
			value := &serviceResource{}
			calls := 0
			probeErr := errors.New("temporarily unavailable")
			observe := func(got context.Context, resource *serviceResource) error {
				calls++
				if got != ctx || resource != value || got.Value(serviceContextKey{}) != "caller" {
					return errors.New("callback context or resource replaced")
				}
				if deadline, ok := got.Deadline(); !ok || deadline.IsZero() {
					return errors.New("deadline lost")
				}
				if got.Err() != nil && !errors.Is(context.Cause(got), cause) {
					return errors.New("cancellation cause lost")
				}
				return nil
			}
			adapter, err := factory(lifecycleOptions{
				resource: value, startup: observe, shutdown: observe,
				readiness: func(ctx context.Context, resource *serviceResource) error {
					return errors.Join(observe(ctx, resource), probeErr)
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			check, ok := adapter.Readiness()
			if !ok {
				t.Fatal("readiness absent")
			}
			if !errors.Is(check.Run(ctx), canonical.ErrUnavailable) || calls != 0 {
				t.Fatal("inactive probe executed")
			}
			component := adapter.Component()
			if err := component.Start(ctx); err != nil {
				t.Fatal(err)
			}
			if !errors.Is(check.Run(ctx), probeErr) {
				t.Fatal("probe failure lost")
			}
			probeErr = nil
			if err := check.Run(ctx); err != nil {
				t.Fatalf("probe did not recover: %v", err)
			}
			cancelCause(cause)
			if err := component.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			if !errors.Is(check.Run(ctx), canonical.ErrUnavailable) || calls != 4 {
				t.Fatal("stopped probe executed or callback omitted")
			}
		})
	}
}

func TestServiceAdapterComposesOwnershipAndStartupRollback(t *testing.T) {
	for name, factory := range cacheLifecycleFactories {
		for _, scenario := range []string{"transferred", "shared", "cache failure", "dependent failure"} {
			t.Run(name+"/"+scenario, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				value := &serviceResource{payload: "cached-value"}
				var events []string
				startupErr, cleanupErr := errors.New("startup marker"), errors.New("cleanup marker")
				options := lifecycleOptions{
					resource: value,
					startup: func(context.Context, *serviceResource) error {
						events = append(events, "cache start")
						if scenario == "cache failure" {
							return startupErr
						}
						return nil
					},
					readiness: func(context.Context, *serviceResource) error { return nil },
				}
				if scenario != "shared" {
					options.shutdown = func(context.Context, *serviceResource) error {
						events = append(events, "cache close")
						value.closed = true
						value.closes++
						if scenario == "cache failure" {
							return cleanupErr
						}
						return nil
					}
				}
				adapter, err := factory(options)
				if err != nil {
					t.Fatal(err)
				}
				dependent := service.Component{
					Name: "dependent",
					Start: func(context.Context) error {
						events = append(events, "dependent start")
						if scenario == "dependent failure" {
							return startupErr
						}
						return nil
					},
					Stop: func(context.Context) error {
						if adapter.Resource() != value || value.closed || value.payload != "cached-value" {
							return errors.New("cache unavailable before dependent drained")
						}
						events = append(events, "dependent stop")
						return nil
					},
				}
				runtime, err := service.New(service.Config{Components: []service.Component{adapter.Component(), dependent}})
				if err != nil {
					t.Fatal(err)
				}
				startErr := runtime.Start(ctx)
				if scenario == "cache failure" || scenario == "dependent failure" {
					if !errors.Is(startErr, startupErr) || runtime.Ready() {
						t.Fatal("startup failure did not prevent readiness")
					}
					if scenario == "cache failure" && !errors.Is(startErr, cleanupErr) {
						t.Fatal("cleanup cause lost")
					}
				} else if startErr != nil || !runtime.Ready() {
					t.Fatalf("Start = %v, ready = %v", startErr, runtime.Ready())
				}
				for range 2 {
					if err := runtime.Shutdown(ctx); err != nil {
						t.Fatal(err)
					}
				}
				check, _ := adapter.Readiness()
				if !errors.Is(check.Run(ctx), canonical.ErrUnavailable) || runtime.State() != service.StateStopped {
					t.Fatal("stopped resource remained available")
				}
				want := []string{"cache start", "dependent start", "dependent stop", "cache close"}
				closes := 1
				switch scenario {
				case "shared":
					want = want[:3]
					closes = 0
				case "cache failure":
					want = []string{"cache start", "cache close"}
				case "dependent failure":
					want = []string{"cache start", "dependent start", "cache close"}
				}
				if !reflect.DeepEqual(events, want) || value.closes != closes {
					t.Fatalf("events = %v, closes = %d; want %v, %d", events, value.closes, want, closes)
				}
			})
		}
	}
}
