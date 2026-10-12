package cache

import (
	"cmp"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// KeyEncoder deterministically converts a typed logical key to bytes.
type KeyEncoder[K any] interface {
	// EncodeKey returns deterministic borrowed key bytes or an error; size/work are caller-bounded.
	EncodeKey(K) ([]byte, error)
}

// StringKeyEncoder encodes strings without conversion.
type StringKeyEncoder struct{}

// EncodeKey returns an owned copy of key bytes without UTF-8 validation.
func (StringKeyEncoder) EncodeKey(key string) ([]byte, error) {
	return []byte(key), nil
}

// KeySpace hashes logical keys beneath a namespace, name, and version prefix.
type KeySpace[K any] struct {
	prefix     string
	encoder    KeyEncoder[K]
	maxKeySize int
}

// NewKeySpace validates and constructs an isolated versioned key space.
func NewKeySpace[K any](
	namespace string,
	name string,
	version uint32,
	encoder KeyEncoder[K],
	maxKeySize int,
) (KeySpace[K], error) {
	if !validKeyPart(namespace) || !validKeyPart(name) || version == 0 || isNilDependency(encoder) || maxKeySize <= 0 {
		return KeySpace[K]{}, ErrInvalidKey
	}
	return KeySpace[K]{
		prefix:     fmt.Sprintf("%s:%s:v%d:", namespace, name, version),
		encoder:    encoder,
		maxKeySize: maxKeySize,
	}, nil
}

// Key hashes encoder bytes beneath the visible prefix. maxKeySize bounds the
// final backend key, not logical input bytes or encoder work. No panic recovery
// is provided. Use NewKeySpace; zero-value spaces have no encoder.
func (s KeySpace[K]) Key(logical K) (string, error) {
	encoded, err := s.encoder.EncodeKey(logical)
	switch err {
	case nil:
	default:
		return "", NewError(InvalidKeyError, Operation("key"), err)
	}
	digest := sha256.Sum256(encoded)
	key := s.prefix + base64.RawURLEncoding.EncodeToString(digest[:])
	switch cmp.Compare(len(key), s.maxKeySize) {
	case 1:
		return "", fmt.Errorf("%w: encoded length %d exceeds %d", ErrKeyTooLarge, len(key), s.maxKeySize)
	}
	return key, nil
}

func validKeyPart(value string) bool {
	switch value {
	case "":
		return false
	}
	if cmp.Compare(len(value), 64) == 1 {
		return false
	}
	switch strings.Contains(value, ":") {
	case true:
		return false
	}
	for _, r := range value {
		switch strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789-_", r) {
		case false:
			return false
		}
	}
	return true
}
