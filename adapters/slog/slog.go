package cacheslog

import (
	"context"
	"fmt"
	logslog "log/slog"

	cache "github.com/faustbrian/go-cache/v3"
)

// Config selects the logger and level used by an Observer.
type Config struct {
	// Logger is retained and caller-owned, including handlers and their blocking behavior.
	Logger *logslog.Logger
	// Level is the level passed to LogAttrs without filtering in this adapter.
	Level logslog.Level
	// IncludeSize adds the size_bytes field when true; zero omits it.
	IncludeSize bool
}

// Observer writes redacted semantic cache events through slog.
type Observer struct {
	logger      *logslog.Logger
	level       logslog.Level
	includeSize bool
}

// New validates config and constructs a logging observer.
func New(config Config) (*Observer, error) {
	if config.Logger == nil {
		return nil, fmt.Errorf("slog observer requires a logger")
	}
	return &Observer{
		logger:      config.Logger,
		level:       config.Level,
		includeSize: config.IncludeSize,
	}, nil
}

// Observe synchronously forwards event fields to the configured logger.
// Unlike OTel it does not validate operation/outcome constants; direct callers
// must supply bounded safe labels, and logger/handler attributes can add data.
// This method does not itself recover handler panics.
func (o *Observer) Observe(ctx context.Context, event cache.Event) error {
	attributes := []logslog.Attr{
		logslog.String("operation", string(event.Operation)),
		logslog.String("outcome", string(event.Outcome)),
		logslog.Float64("duration_ms", float64(event.Duration.Microseconds())/1000),
	}
	if o.includeSize {
		attributes = append(attributes, logslog.Int("size_bytes", event.Size))
	}
	o.logger.LogAttrs(ctx, o.level, "cache operation", attributes...)
	return nil
}
