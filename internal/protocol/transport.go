package protocol

import "time"

// Request is one HTTP round-trip against a pipeline server. HostPort and
// AuthToken live per-request (not per-transport) so a client can transparently
// follow a domain reload that rewrites the port and the eval token.
type Request struct {
	Method    string        // "GET" or "POST"
	Path      string        // e.g. "/api/exec" — may carry a query string
	Body      []byte        // JSON body; nil for GET
	HostPort  string        // e.g. "127.0.0.1:7802"
	AuthToken string        // Bearer token; empty = no auth header
	Timeout   time.Duration // per-request timeout; 0 = transport default
}

// Response is the raw HTTP response.
type Response struct {
	StatusCode int
	Body       []byte
}

// Transport is the only I/O seam of the protocol layer. Implementations live
// in internal/transport (net/http) — protocol code depends on this interface
// alone, which keeps the whole layer unit-testable with fakes and mechanically
// portable.
type Transport interface {
	Do(req Request) (Response, error)
	Close() error
}
