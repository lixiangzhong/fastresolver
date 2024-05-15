package fastresolver

import (
	"context"

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
func (r *RateLimitResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	r.Take()
	return r.resolver.Lookup(ctx, name, qtype)
}
