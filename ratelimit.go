package fastresolver

import (
	"context"

	"go.uber.org/ratelimit"
)

func NewRateLimitResolver(r Resolver, qps int) Resolver {
	return &RateLimitResolver{
		Limiter:  ratelimit.New(qps),
		resolver: r,
	}
}

var _ Resolver = (*RateLimitResolver)(nil)

type RateLimitResolver struct {
	ratelimit.Limiter
	resolver Resolver
}

// LookupIP implements Resolver.
func (r *RateLimitResolver) LookupIP(ctx context.Context, name string) ([]string, error) {
	r.Take()
	return r.resolver.LookupIP(ctx, name)
}

// LookupNS implements Resolver.
func (r *RateLimitResolver) LookupNS(ctx context.Context, name string) ([]string, error) {
	r.Take()
	return r.resolver.LookupNS(ctx, name)
}
