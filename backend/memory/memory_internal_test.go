package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	cache "github.com/faustbrian/go-cache"
)

func TestSetRejectsEmptyEvictionList(t *testing.T) {
	backend, err := New(Config{MaxEntries: 1, MaxBytes: 1024, Clock: cache.SystemClock{}})
	if err != nil {
		t.Fatal(err)
	}

	element := backend.lru.PushFront(&entry{key: "orphan"})
	backend.lru.Remove(element)
	backend.items["orphan"] = element

	_, err = backend.Set(context.Background(), "key", cache.Record{
		Payload:   []byte("value"),
		ExpiresAt: time.Now().Add(time.Minute),
		StaleAt:   time.Now().Add(2 * time.Minute),
	}, cache.Unconditional)
	if !errors.Is(err, cache.ErrCapacity) {
		t.Fatalf("Set() error = %v, want ErrCapacity", err)
	}
}
