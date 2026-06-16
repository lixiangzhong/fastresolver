package fastresolver

import (
	"context"
	"math/rand"
	"sync/atomic"
)

type LoadBalancer interface {
	Choose([]ILookup) ILookup
}

type LoadBalanceResolver struct {
	lb        LoadBalancer
	resolvers []ILookup
}

func NewLoadBalanceResolver(lb LoadBalancer, resolvers ...ILookup) *LoadBalanceResolver {
	return &LoadBalanceResolver{
		lb:        lb,
		resolvers: resolvers,
	}
}

// Lookup implements ILookup.
func (b *LoadBalanceResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	if len(b.resolvers) == 0 {
		return DNSRR{}, ErrNoResolver
	}
	resolver := b.lb.Choose(b.resolvers)
	if resolver == nil {
		return DNSRR{}, ErrNoResolver
	}
	return resolver.Lookup(ctx, name, qtype)
}

type RoundRobinBalancer struct {
	idx atomic.Uint64
}

func NewRoundRobinBalancer() *RoundRobinBalancer {
	return &RoundRobinBalancer{}
}

func (r *RoundRobinBalancer) Choose(resolvers []ILookup) ILookup {
	n := len(resolvers)
	if n == 0 {
		return nil
	}
	var idx int
	start := int(r.idx.Add(1)-1) % n
	for i := 0; i < n; i++ {
		idx = (start + i) % n
		if cb, ok := resolvers[idx].(CircuitBreaker); ok {
			if cb.Accept() {
				break
			}
		} else {
			break
		}
	}
	return resolvers[idx]
}

type RandomBalancer struct{}

func NewRandomBalancer() *RandomBalancer {
	return &RandomBalancer{}
}

func (r *RandomBalancer) Choose(resolvers []ILookup) ILookup {
	n := len(resolvers)
	if n == 0 {
		return nil
	}
	idx := rand.Intn(n) % n
	for i := 0; i < n; i++ {
		idx = (idx + i) % n
		if cb, ok := resolvers[idx].(CircuitBreaker); ok {
			if cb.Accept() {
				break
			}
		} else {
			break
		}
	}
	return resolvers[idx]
}
