# Codecs and schema evolution

[Documentation index](README.md) · [Repository entry](../README.md)

The built-in `JSONCodec[V]` encodes a one-byte nonzero version followed by JSON.
Encode rejects a zero configured version; Decode only compares the
incoming version and can accept zero when configured zero. json.Marshal and
custom encoding work occur before the encoded-size check. Decode checks wire
size before JSON parsing. Unknown struct fields, trailing values, malformed
data and version mismatch fail explicitly; maps and custom methods retain
encoding/json semantics.

Go map keys supported by `encoding/json` are sorted, so equivalent map values
produce deterministic bytes. A nil pointer encodes as JSON `null` and remains a
valid hit. Empty raw bytes are a schema mismatch, not a nil value or tombstone.

Choose a codec version for the exact Go value schema. When a deployed reader
cannot safely decode old bytes, increment the codec version and normally also
increment the key-space version. The key version prevents repeatedly reading
known-incompatible data; the codec version remains a corruption and rollout
safety check.

Custom codecs should:

- deterministically encode equivalent values;
- preserve zero values without a sentinel conversion;
- carry an explicit schema/version marker;
- reject oversized input before allocating from untrusted lengths;
- reject trailing or unknown data unless the schema explicitly permits it;
- return errors matching `ErrDecode`, `ErrSchemaMismatch`, or
  `ErrValueTooLarge` as appropriate;
- never silently coerce or truncate values.

Treat backend bytes as untrusted even when the backend is private. Mixed
deployments, manual writes, and stale records can all produce incompatible
payloads.

The version byte identifies an application schema, not a Go type at runtime.
Never reuse one key space and codec version for different semantic types even
if their current JSON shapes happen to decode successfully. The built-in codec
does not compress, so decompression bombs do not apply; allocation and parsing
are constrained by input bytes, not a total heap/CPU bound. Custom JSON methods
can perform additional work; field matching, duplicate keys, maps and nulls
retain encoding/json semantics. Unknown struct fields are rejected, not every
possible unknown application field.

Choose the next guide in the [documentation index](README.md)
or return to the [repository entry](../README.md) for installation and ownership.
