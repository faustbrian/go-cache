// Package wire encodes portable backend records for remote storage.
// The private GCH version-1 envelope has a 21-byte header with big-endian signed
// Unix-nanosecond bit patterns and an optional negative flag, followed by payload.
// Decode returns copied bytes; no compression or total heap limit is supplied.
package wire
