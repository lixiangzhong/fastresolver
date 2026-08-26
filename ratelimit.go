package fastresolver

import (
	"context"
	"fmt"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/time/rate"
)

var _ ILookup = (*RateLimitResolver)(nil)

type RateLimitResolver struct {
	limiter  *rate.Limiter
	resolver ILookup
}

// NewRateLimitResolver creates a strict per-resolver QPS limiter.
func NewRateLimitResolver(r ILookup, qps int) *RateLimitResolver {
	if qps <= 0 {
		panic("rate limit QPS must be positive")
	}
	return &RateLimitResolver{
		limiter:  rate.NewLimiter(rate.Limit(qps), 1),
		resolver: r,
	}
}

// Lookup implements ILookup.
func (r *RateLimitResolver) Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := time.Now()
	reservation := r.limiter.ReserveN(now, 1)
	if !reservation.OK() {
		return nil, fmt.Errorf("reserve rate limit token")
	}
	delay := reservation.DelayFrom(now)
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			reservation.CancelAt(time.Now())
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return normalizeLookupResult(r.resolver.Lookup(ctx, name, qtype))
}

// Unwrap returns the underlying resolver.
func (r *RateLimitResolver) Unwrap() ILookup {
	return r.resolver
}
