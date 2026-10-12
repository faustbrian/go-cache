// Package cacheredis adapts go-redis/v9 clients to the cache backend contract.
// Clients remain caller-owned; configure their TLS, retries and deadlines.
// Writes use relative server expiry to StaleAt, with a 1 ms minimum; clock skew,
// transport latency and provider retry semantics remain observable. Constructors
// do not ping/open/close clients. Standalone is the documented supported topology.
package cacheredis
