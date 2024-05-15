package fastresolver

import (
	"context"
	"math/rand"
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
	return b.lb.Choose(b.resolvers).Lookup(ctx, name, qtype)
}

type RoundRobinBalancer struct {
	idx int
}

func NewRoundRobinBalancer() *RoundRobinBalancer {
	return &RoundRobinBalancer{}
}

func (r *RoundRobinBalancer) Choose(resolvers []ILookup) ILookup {
	n := len(resolvers)
	var idx int
	for i := 0; i < n; i++ {
		idx = (r.idx + i) % n
		if cb, ok := resolvers[idx].(CircuitBreaker); ok {
			if cb.Accept() {
				break
			}
		} else {
			break
		}
	}
	r.idx++
	return resolvers[idx]
}

type RandomBalancer struct{}

func NewRandomBalancer() *RandomBalancer {
	return &RandomBalancer{}
}

func (r *RandomBalancer) Choose(resolvers []ILookup) ILookup {
	n := len(resolvers)
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
