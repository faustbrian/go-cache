package cache

import "context"

// Entry pairs a logical key with a value for SetMany.
type Entry[K, V any] struct {
	// Key is the logical key, borrowed without generic deep copying.
	Key K
	// Value is borrowed for serialization during its sequential Set.
	Value V
}

// BulkResult reports one GetMany result without flattening per-key errors.
type BulkResult[K, V any] struct {
	// Key is the input key at the corresponding position.
	Key K
	// Result is the embedded lookup outcome; inspect Err before using it.
	Result[V]
	// Err is this key's failure; the bulk-level error only reports admission failure.
	Err error
}

// MutationResult reports one bulk mutation error in input order.
type MutationResult[K any] struct {
	// Key is the input key at the corresponding position.
	Key K
	// Err is this mutation's failure; other items are still attempted.
	Err error
}

// GetMany reads keys sequentially in input order without a pipeline/transaction.
// Admission failure is the top-level error; per-key errors do not stop the loop,
// even after cancellation. Generic keys/results are not deep copied.
func (c *Cache[K, V]) GetMany(ctx context.Context, keys []K) ([]BulkResult[K, V], error) {
	if err := c.validateBatch(len(keys)); err != nil {
		return nil, err
	}
	results := make([]BulkResult[K, V], len(keys))
	for index, key := range keys {
		result, err := c.Get(ctx, key)
		results[index] = BulkResult[K, V]{Key: key, Result: result, Err: err}
	}
	return results, nil
}

// SetMany writes entries sequentially in input order and records per-key errors.
// There is no rollback, transaction or early stop after a failed/canceled item.
func (c *Cache[K, V]) SetMany(ctx context.Context, entries []Entry[K, V]) ([]MutationResult[K], error) {
	if err := c.validateBatch(len(entries)); err != nil {
		return nil, err
	}
	results := make([]MutationResult[K], len(entries))
	for index, entry := range entries {
		results[index] = MutationResult[K]{Key: entry.Key, Err: c.Set(ctx, entry.Key, entry.Value)}
	}
	return results, nil
}

// DeleteMany deletes keys sequentially in input order with per-key failures.
// There is no transaction or early stop after a failed/canceled item.
func (c *Cache[K, V]) DeleteMany(ctx context.Context, keys []K) ([]MutationResult[K], error) {
	if err := c.validateBatch(len(keys)); err != nil {
		return nil, err
	}
	results := make([]MutationResult[K], len(keys))
	for index, key := range keys {
		results[index] = MutationResult[K]{Key: key, Err: c.Delete(ctx, key)}
	}
	return results, nil
}

func (c *Cache[K, V]) validateBatch(size int) error {
	if size > c.maxBatch {
		return ErrBatchTooLarge
	}
	return nil
}
