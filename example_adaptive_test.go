package fastresolver_test

import (
	"context"
	"time"

	fastresolver "github.com/lixiangzhong/fastresolver/v3"
	"github.com/miekg/dns"
)

func ExampleNewAdaptivePoolResolver() {
	addresses := []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}
	upstreams := make([]fastresolver.ILookup, 0, len(addresses))
	for _, address := range addresses {
		resolver, err := fastresolver.NewResolverWithTimeout(address, 2*time.Second)
		if err != nil {
			return
		}
		upstreams = append(upstreams, resolver)
	}

	pool, err := fastresolver.NewAdaptivePoolResolver(
		upstreams,
		fastresolver.WithAdaptiveMaxQPS(500),
		fastresolver.WithAdaptiveMaxAttempts(3),
	)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	response, err := pool.Lookup(ctx, "example.com", dns.TypeA)
	if err != nil {
		return
	}
	_ = response.Answer
	_ = pool.Stats()
}
