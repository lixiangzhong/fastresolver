package fastresolver

import "errors"

type ServerRefusedError struct {
	Qname  string
	Server string
}

func (e ServerRefusedError) Error() string {
	return "server " + e.Server + " refused query for " + e.Qname
}

type TruncatedError struct {
	Qname  string
	Server string
}

func (e TruncatedError) Error() string {
	return "server " + e.Server + " truncated query for " + e.Qname
}

var ErrCircuitBreaker = errors.New("circuit breaker")

// ErrNoResolver indicates that no resolver is available for lookup.
var ErrNoResolver = errors.New("no resolver")

// ErrCnameDepthExceeded indicates that CNAME following exceeded the safe recursion limit.
var ErrCnameDepthExceeded = errors.New("cname depth exceeded")
