package fastresolver

import (
	"context"
	"sync/atomic"
)

var _ ILookup = (*MetricsResolver)(nil)

type MetricsResolver struct {
	resolver ILookup
	success  *atomic.Uint64
	failure  *atomic.Uint64
}

func NewMetricsResolver(resolver ILookup) *MetricsResolver {
	return &MetricsResolver{
		resolver: resolver,
		success:  new(atomic.Uint64),
		failure:  new(atomic.Uint64),
	}
}

// Lookup implements ILookup.
func (m *MetricsResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	ret, err := m.resolver.Lookup(ctx, name, qtype)
	if err != nil {
		m.failure.Add(1)
	} else {
		m.success.Add(1)
	}
	return ret, err
}

type CircuitBreaker interface {
	Accept() bool
}

var _ CircuitBreaker = (*CircuitBreakerResolver)(nil)
var _ ILookup = (*CircuitBreakerResolver)(nil)

type CircuitBreakerResolver struct {
	resolver         *MetricsResolver
	failureThreshold uint64
}

func NewCircuitBreakerResolver(resolver ILookup, failureThreshold uint64) *CircuitBreakerResolver {
	return &CircuitBreakerResolver{
		resolver:         NewMetricsResolver(resolver),
		failureThreshold: failureThreshold,
	}
}

func (c *CircuitBreakerResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	if c.resolver.failure.Load() >= c.failureThreshold {
		return DNSRR{}, ErrCircuitBreaker
	}
	return c.resolver.Lookup(ctx, name, qtype)
}

// Accept implements CircuitBreaker.
func (c *CircuitBreakerResolver) Accept() bool {
	return c.resolver.failure.Load() < c.failureThreshold
}
