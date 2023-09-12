package fastresolver

import (
	"context"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/miekg/dns"
)

var DefalutCache = NewLRU(50000, time.Minute)

type Cache interface {
	Set(name string, qtype uint16, answer []string)
	Get(name string, qtype uint16) ([]string, bool)
}

func NewCacheResolver(cache Cache, resolver Resolver) Resolver {
	return &cacheResovler{cache: cache, resolver: resolver}
}

var _ Resolver = (*cacheResovler)(nil)

type cacheResovler struct {
	cache    Cache
	resolver Resolver
}

// Lookup implements Resolver.
func (c *cacheResovler) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	val, ok := c.cache.Get(name, qtype)
	if ok {
		return DNSRR{A: val}, nil
	}
	ret, err := c.resolver.Lookup(ctx, name, qtype)
	if err != nil {
		return ret, err
	}
	switch qtype {
	case dns.TypeA:
		c.cache.Set(name, qtype, ret.A)
	case dns.TypeCNAME:
		c.cache.Set(name, qtype, ret.CNAME)
	case dns.TypeNS:
		c.cache.Set(name, qtype, ret.NS)
	}
	return ret, nil
}

// LookupIP implements Resolver.
func (c *cacheResovler) LookupIP(ctx context.Context, name string) ([]string, error) {
	val, ok := c.cache.Get(name, dns.TypeA)
	if ok {
		return val, nil
	}
	val, err := c.resolver.LookupIP(ctx, name)
	if err != nil {
		return nil, err
	}
	c.cache.Set(name, dns.TypeA, val)
	return val, nil
}

// LookupNS implements Resolver.
func (c *cacheResovler) LookupNS(ctx context.Context, name string) ([]string, error) {
	val, ok := c.cache.Get(name, dns.TypeNS)
	if ok {
		return val, nil
	}
	val, err := c.resolver.LookupNS(ctx, name)
	if err != nil {
		return nil, err
	}
	c.cache.Set(name, dns.TypeNS, val)
	return val, nil
}

type cacheKey struct {
	name  string
	qtype uint16
}

var _ Cache = (*memLRU)(nil)

type memLRU struct {
	cache *expirable.LRU[cacheKey, []string]
}

func NewLRU(size int, ttl time.Duration) Cache {
	store := expirable.NewLRU[cacheKey, []string](size, nil, ttl)
	return &memLRU{cache: store}
}

// Get implements Cache.
func (m *memLRU) Get(name string, qtype uint16) ([]string, bool) {
	k := cacheKey{name: name, qtype: qtype}
	return m.cache.Get(k)
}

// Set implements Cache.
func (m *memLRU) Set(name string, qtype uint16, answer []string) {
	k := cacheKey{name: name, qtype: qtype}
	m.cache.Add(k, answer)
}
