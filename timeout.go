package fastresolver

import (
	"context"
	"time"

	"github.com/miekg/dns"
)

var _ ILookup = (*TimeoutResolver)(nil)

// TimeoutResolver is a decorator that injects a timeout into the context
// of each lookup request to unify context-driven timeout management.
type TimeoutResolver struct {
	timeout  time.Duration
	resolver ILookup
}

// NewTimeoutResolver creates a new TimeoutResolver.
func NewTimeoutResolver(resolver ILookup, timeout time.Duration) *TimeoutResolver {
	return &TimeoutResolver{
		timeout:  timeout,
		resolver: resolver,
	}
}

// Lookup implements ILookup.
func (t *TimeoutResolver) Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	response, err := normalizeLookupResult(t.resolver.Lookup(ctx, name, qtype))
	if ctxErr := ctx.Err(); ctxErr != nil {
		return response, ctxErr
	}
	return response, err
}

// Unwrap returns the underlying resolver.
func (t *TimeoutResolver) Unwrap() ILookup {
	return t.resolver
}
