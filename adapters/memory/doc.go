// Package cachememory provides a bounded, concurrency-safe LRU cache adapter.
// Entry/byte accounting is not a heap cap. Expiration is lazy at StaleAt, and
// Stats does not sweep. Close releases retained entries; no janitor is started.
// Clock callbacks run under the mutex; observers run synchronously after it.
package cachememory
