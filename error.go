package fastresolver

import "errors"

type ServerRefusedError struct {
	Qname  string
	Server string
}

func (e ServerRefusedError) Error() string {
	return "server " + e.Server + " refused query for " + e.Qname
}

// ServerFailureError indicates that an upstream returned SERVFAIL.
type ServerFailureError struct {
	Qname  string
	Server string
}

func (e ServerFailureError) Error() string {
	return "server " + e.Server + " failed query for " + e.Qname
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

// ErrNoResponse indicates that a resolver returned neither a response nor an error.
var ErrNoResponse = errors.New("resolver returned no response")

// ErrCnameDepthExceeded indicates that CNAME following exceeded the safe recursion limit.
var ErrCnameDepthExceeded = errors.New("cname depth exceeded")

// ErrMaxRecursionDepth indicates that recursive lookup exceeded the maximum depth.
var ErrMaxRecursionDepth = errors.New("maximum recursion depth exceeded")

// ErrNoQuestion indicates that the DNS response has an empty question section,
// which happens when an upstream server returns a malformed or unexpected reply.
var ErrNoQuestion = errors.New("dns response has no question section")

// ErrInvalidQuestion indicates that the DNS response question doesn't match the request.
var ErrInvalidQuestion = errors.New("invalid DNS response question")

// ErrInvalidResponseID indicates that a protocol response used an unexpected message ID.
var ErrInvalidResponseID = errors.New("invalid DNS response ID")
