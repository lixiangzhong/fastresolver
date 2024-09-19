package fastresolver

import (
	"net"
	"net/netip"
	"strings"
)

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

func cacheNetLookupIP(qname string) (DNSRR, error) {
	rr, ok := DefalutMemCache.Get(qname, 0) //不区分a还是aaaa
	if ok {
		return rr, nil
	}
	ips, err := net.LookupIP(qname)
	if err != nil {
		if strings.Contains(err.Error(), "no such host") {
			rr.NXDomain = true
			DefalutMemCache.Set(qname, 0, rr)
			return rr, nil
		}
		return rr, err
	}
	for _, v := range ips {
		ip, ok := netip.AddrFromSlice(v)
		if !ok {
			continue
		}
		if ip.Is6() {
			rr.AAAA = append(rr.AAAA, ip.String())
		}
		if ip.Is4() {
			rr.A = append(rr.A, ip.String())
		}
	}
	DefalutMemCache.Set(qname, 0, rr)
	return rr, nil
}
