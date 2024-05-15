package fastresolver

func Default() ILookup {
	famous := []string{
		"1.1.1.1", "1.0.0.1",
		"8.8.8.8", "8.8.4.4",
		"114.114.114.114", "114.114.115.115",
		"223.5.5.5", "223.6.6.6",
		"119.29.29.29",
	}

	var resolvers []ILookup
	for _, addr := range famous {
		var r ILookup
		r, err := NewResolver(addr)
		if err != nil {
			continue
		}
		r = NewRateLimitResolver(r, 100)
		r = NewCircuitBreakerResolver(r, 100)
		resolvers = append(resolvers, r)
	}
	var resolver ILookup
	resolver = NewLoadBalanceResolver(NewRandomBalancer(), resolvers...)
	resolver = NewRetryResolver(3, resolver)
	resolver = NewCacheResolver(DefalutMemCache, resolver)
	resolver = NewFollowCnameResolver(resolver)
	return resolver
}
