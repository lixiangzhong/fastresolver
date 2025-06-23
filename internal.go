package fastresolver

import (
	"strings"
	"time"
)

var internalResolver ILookup

func init() {
	dns := strings.Fields(`
1.0.0.1
1.1.1.1
8.8.8.8
8.8.4.4
114.114.114.114
114.114.115.115
223.5.5.5
223.6.6.6
119.29.29.29
`)
	var resolvers []ILookup
	for _, addr := range dns {
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
	resolver = NewCacheResolver(NewLRU(50000, time.Minute), resolver)
	resolver = NewFollowCnameResolver(resolver)
	internalResolver = resolver
}
