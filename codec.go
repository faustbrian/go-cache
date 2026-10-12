package cache

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
)

const defaultMaxEncodedSize = 1 << 20

// Codec serializes and deserializes typed cache values. It is a retained
// concurrency-safe collaborator, not a generic deep-copy or panic boundary.
// Decode receives the retrieved record payload; resulting V ownership is defined
// by the codec. Cache does not clone its returned value.
type Codec[V any] interface {
	// Encode serializes a borrowed V; callers must not mutate it during encoding.
	Encode(V) ([]byte, error)
	// Decode reads borrowed bytes and returns V with codec-defined ownership.
	Decode([]byte) (V, error)
}

// JSONCodec stores encoding/json data behind a one-byte schema version.
// Decode disallows unknown struct fields and trailing documents; map/interface
// fields and custom JSON methods retain encoding/json semantics. Version zero
// is rejected by Encode, not independently by Decode. The size cap is a wire
// bound, not a total heap/CPU bound or limit on user marshaling callbacks.
type JSONCodec[V any] struct {
	// Version is the schema byte; Encode rejects zero, Decode only compares it.
	Version byte
	// MaxEncodedSize bounds bytes including the version; non-positive selects 1 MiB.
	MaxEncodedSize int
}

// Encode serializes a value with its nonzero configured schema version.
// json.Marshal and custom marshaling work occur before the encoded-size check.
func (c JSONCodec[V]) Encode(value V) ([]byte, error) {
	if c.Version == 0 {
		return nil, fmt.Errorf("%w: version must be non-zero", ErrSchemaMismatch)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, NewError(DecodeError, OperationSet, err)
	}
	limit := c.sizeLimit()
	if len(payload) >= limit {
		return nil, fmt.Errorf(
			"%w: payload length %d leaves no room within encoded limit %d",
			ErrValueTooLarge,
			len(payload),
			limit,
		)
	}
	encodedSize := len(payload) + 1
	encoded := make([]byte, encodedSize)
	encoded[0] = c.Version
	copy(encoded[1:], payload)
	return encoded, nil
}

// Decode validates size and schema version before strict JSON decoding.
func (c JSONCodec[V]) Decode(encoded []byte) (V, error) {
	var zero V
	if cmp.Compare(len(encoded), c.sizeLimit()) == 1 {
		return zero, fmt.Errorf("%w: encoded length %d exceeds %d", ErrValueTooLarge, len(encoded), c.sizeLimit())
	}
	if len(encoded) == 0 || encoded[0] != c.Version {
		return zero, fmt.Errorf("%w: got version %d, want %d", ErrSchemaMismatch, versionOf(encoded), c.Version)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded[1:]))
	decoder.DisallowUnknownFields()
	var value V
	if err := decoder.Decode(&value); err != nil {
		return zero, NewError(DecodeError, OperationGet, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return zero, fmt.Errorf("%w: trailing data", ErrDecode)
	}
	return value, nil
}

func (c JSONCodec[V]) sizeLimit() int {
	if c.MaxEncodedSize <= 0 {
		return defaultMaxEncodedSize
	}
	return c.MaxEncodedSize
}

func versionOf(encoded []byte) byte {
	if len(encoded) == 0 {
		return 0
	}
	return encoded[0]
}
