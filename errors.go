package cache

import (
	"errors"
	"fmt"
)

var (
	// ErrMiss identifies an explicit cache miss where an error value is needed.
	ErrMiss = errors.New("cache miss")
	// ErrBackend identifies a storage or transport failure.
	ErrBackend = errors.New("cache backend error")
	// ErrDecode identifies malformed serialized data.
	ErrDecode = errors.New("cache decode error")
	// ErrSchemaMismatch identifies an incompatible payload version.
	ErrSchemaMismatch = errors.New("cache schema mismatch")
	// ErrInvalidKey identifies invalid key configuration or encoding.
	ErrInvalidKey = errors.New("invalid cache key")
	// ErrKeyTooLarge identifies a backend key beyond its configured bound.
	ErrKeyTooLarge = errors.New("cache key too large")
	// ErrValueTooLarge identifies a payload beyond its configured bound.
	ErrValueTooLarge = errors.New("cache value too large")
	// ErrInvalidTTL identifies an invalid or already expired deadline.
	ErrInvalidTTL = errors.New("invalid cache TTL")
	// ErrCapacity identifies a record that cannot fit a bounded backend.
	ErrCapacity = errors.New("cache capacity exceeded")
	// ErrClosed identifies use after cache or backend shutdown.
	ErrClosed = errors.New("cache backend closed")
	// ErrShutdownIncomplete identifies shutdown ending with active loader work.
	ErrShutdownIncomplete = errors.New("cache shutdown incomplete")
	// ErrLoader identifies a source loader failure.
	ErrLoader = errors.New("cache loader error")
	// ErrLoaderPanic identifies a recovered loader panic.
	ErrLoaderPanic = errors.New("cache loader panic")
	// ErrRecursiveLoad identifies a loader re-entering the same cache.
	ErrRecursiveLoad = errors.New("recursive cache load")
	// ErrWaiterLimit identifies excess callers for one active key flight.
	ErrWaiterLimit = errors.New("cache waiter limit exceeded")
	// ErrInvalidPolicy identifies invalid or contradictory policy options.
	ErrInvalidPolicy = errors.New("invalid cache policy")
	// ErrBatchTooLarge identifies a bulk request beyond its configured bound.
	ErrBatchTooLarge = errors.New("cache batch too large")
	// ErrInvalidRecord identifies malformed portable backend state.
	ErrInvalidRecord = errors.New("invalid cache record")
	// ErrInvalidConfig identifies invalid constructor dependencies or limits.
	ErrInvalidConfig = errors.New("invalid cache configuration")
	// ErrOwnershipLost identifies a protected write rejected by its backend.
	ErrOwnershipLost = errors.New("cache ownership lost")
	// ErrOwnershipUnsupported identifies a backend without atomic ownership validation.
	ErrOwnershipUnsupported = errors.New("cache ownership validation unsupported")
)

// ErrorKind classifies an operation failure independently of its cause.
type ErrorKind uint8

const (
	// BackendError identifies storage or transport failures.
	BackendError ErrorKind = iota + 1
	// DecodeError identifies malformed encoded values.
	DecodeError
	// SchemaMismatchError identifies incompatible payload versions.
	SchemaMismatchError
	// InvalidKeyError identifies invalid key configuration or encoding.
	InvalidKeyError
	// LimitError identifies a configured resource-limit violation.
	LimitError
	// PolicyError identifies an invalid or contradictory policy.
	PolicyError
	// LoaderError identifies a source loader failure.
	LoaderError
)

// Operation names the semantic cache action associated with an event or error.
type Operation string

const (
	// OperationGet identifies a read.
	OperationGet Operation = "get"
	// OperationSet identifies a write.
	OperationSet Operation = "set"
	// OperationDelete identifies invalidation.
	OperationDelete Operation = "delete"
	// OperationLoad identifies a source load.
	OperationLoad Operation = "load"
	// OperationEvict identifies capacity eviction.
	OperationEvict Operation = "evict"
	// OperationExpire identifies deadline expiration.
	OperationExpire Operation = "expire"
)

// Error combines a stable semantic kind with a protected cause identity.
// Construct errors with NewError when a source cause must remain recognizable
// through errors.Is without exposing its text or concrete type.
type Error struct {
	Kind      ErrorKind
	Operation Operation
	cause     error
}

type protectedCause struct {
	cause error
}

func (cause *protectedCause) Error() string { return "sensitive cache error" }

func (cause *protectedCause) Is(target error) bool {
	return cause != nil && errors.Is(cause.cause, target)
}

// NewError creates a classified error whose cause remains recognizable with
// errors.Is but is hidden from formatting and errors.As.
func NewError(kind ErrorKind, operation Operation, cause error) *Error {
	return &Error{Kind: kind, Operation: operation, cause: protectCause(cause)}
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	message := fmt.Sprintf("cache %s failed", e.Operation)
	if sentinel := sentinelForKind(e.Kind); sentinel != nil {
		return fmt.Sprintf("%s: %v", message, sentinel)
	}
	return message
}

func (e *Error) Unwrap() []error {
	if e == nil {
		return nil
	}
	sentinel := sentinelForKind(e.Kind)
	if sentinel == nil {
		if e.cause == nil {
			return nil
		}
		return []error{e.cause}
	}
	if e.cause == nil {
		return []error{sentinel}
	}
	return []error{sentinel, e.cause}
}

func protectCause(cause error) error {
	if cause == nil {
		return nil
	}
	return &protectedCause{cause: cause}
}

func sentinelForKind(kind ErrorKind) error {
	switch kind {
	case BackendError:
		return ErrBackend
	case DecodeError:
		return ErrDecode
	case SchemaMismatchError:
		return ErrSchemaMismatch
	case InvalidKeyError:
		return ErrInvalidKey
	case LimitError:
		return ErrValueTooLarge
	case PolicyError:
		return ErrInvalidPolicy
	case LoaderError:
		return ErrLoader
	default:
		return nil
	}
}
