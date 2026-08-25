package fastresolver

import (
	"context"

	"github.com/miekg/dns"
	"go.uber.org/ratelimit"
)

var _ ILookup = (*RateLimitResolver)(nil)

type RateLimitResolver struct {
	ratelimit.Limiter
	resolver ILookup
}

func NewRateLimitResolver(r ILookup, qps int) *RateLimitResolver {
	return &RateLimitResolver{
		Limiter:  ratelimit.New(qps),
		resolver: r,
	}
}

// Lookup implements ILookup.
func (r *RateLimitResolver) Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	r.Take()
	return normalizeLookupResult(r.resolver.Lookup(ctx, name, qtype))
}

// Unwrap returns the underlying resolver.
func (r *RateLimitResolver) Unwrap() ILookup {
	return r.resolver
}
