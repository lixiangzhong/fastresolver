package fastresolver

var DefaultResovler Resolver

func init() {
	DefaultResovler = NewFastResolver(
		Upstream{Addr: "1.1.1.1"},
		Upstream{Addr: "8.8.8.8"},
		Upstream{Addr: "114.114.114.114"},
	)
}

func NewFastResolver(rs ...Resolver) Resolver {
	for i, v := range rs {
		rs[i] = NewRateLimitResolver(v, 100)
	}
	return NewCacheResolver(DefalutCache, NewRetryResolver(3, NewFailoverResovler(100, rs...)))
}
