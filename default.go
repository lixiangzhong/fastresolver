package fastresolver

import (
	"net"
	"net/netip"
	"strings"
)

// https://github.com/trickest/resolvers/blob/main/resolvers-trusted.txt
const famousDNS = `1.0.0.1
1.1.1.1
134.195.4.2
149.112.112.112
159.89.120.99
185.228.168.9
185.228.169.9
195.46.39.39
195.46.39.40
205.171.2.65
205.171.3.65
208.67.220.220
208.67.222.222
216.146.35.35
216.146.36.36
64.6.64.6
64.6.65.6
74.82.42.42
76.76.10.0
76.76.2.0
77.88.8.1
77.88.8.8
8.20.247.20
8.26.56.26
8.8.4.4
8.8.8.8
84.200.69.80
84.200.70.40
89.233.43.71
9.9.9.9
91.239.100.100`

const famousDNSChina = `
114.114.114.114
114.114.115.115
223.5.5.5
223.6.6.6
119.29.29.29
`

func Default() ILookup {
	famous := append(strings.Fields(famousDNSChina), strings.Fields(famousDNS)...)
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
