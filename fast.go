package fastresolver

var DefaultResovler Resolver

func init() {
	DefaultResovler = NewFastResolver(
		Upstream{Addr: "1.1.1.1"},
		Upstream{Addr: "1.0.0.1"},
		Upstream{Addr: "8.8.8.8"},
		Upstream{Addr: "8.8.4.4"},
		Upstream{Addr: "114.114.114.114"},
		Upstream{Addr: "114.114.115.115"},
		Upstream{Addr: "223.5.5.5"},
		Upstream{Addr: "223.6.6.6"},
	)
}

func NewFastResolver(rs ...Resolver) Resolver {
	for i, v := range rs {
		rs[i] = NewRateLimitResolver(v, 100)
	}
	return NewCacheResolver(DefalutCache, NewRetryResolver(3, NewFailoverResovler(100, rs...)))
}
