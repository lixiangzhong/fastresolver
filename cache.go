package fastresolver

import (
	"context"
	"fmt"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"
)

var DefalutMemCache = NewLRU(50000, time.Minute)

type Cache interface {
	Set(name string, qtype uint16, answer DNSRR)
	Get(name string, qtype uint16) (DNSRR, bool)
}

type cacheKey struct {
	name  string
	qtype uint16
}

type cacheItem struct {
	value     DNSRR
	expiredAt time.Time
}

var _ Cache = (*memLRU)(nil)

type memLRU struct {
	cache      *lru.Cache[cacheKey, cacheItem]
	defaultTTL time.Duration
}

func NewLRU(size int, ttl time.Duration) Cache {
	store, _ := lru.New[cacheKey, cacheItem](size)
	return &memLRU{
		cache:      store,
		defaultTTL: ttl,
	}
}

// Get implements Cache.
func (m *memLRU) Get(name string, qtype uint16) (DNSRR, bool) {
	k := cacheKey{name: name, qtype: qtype}
	item, ok := m.cache.Get(k)
	if !ok {
		return DNSRR{}, false
	}
	if time.Now().After(item.expiredAt) {
		m.cache.Remove(k)
		return DNSRR{}, false
	}
	return item.value, true
}

// Set implements Cache.
func (m *memLRU) Set(name string, qtype uint16, answer DNSRR) {
	k := cacheKey{name: name, qtype: qtype}
	ttl := answer.TTL
	if ttl == 0 {
		ttl = m.defaultTTL
	}
	// Enforce a minimum TTL of 1 minute.
	if ttl < time.Minute {
		ttl = time.Minute
	}
	m.cache.Add(k, cacheItem{
		value:     answer,
		expiredAt: time.Now().Add(ttl),
	})
}

var _ ILookup = (*CacheResolver)(nil)

type CacheResolver struct {
	cache    Cache
	resolver ILookup
	sfGroup  singleflight.Group
}

func NewCacheResolver(cache Cache, resolver ILookup) *CacheResolver {
	return &CacheResolver{
		cache:    cache,
		resolver: resolver,
	}
}

// Lookup implements ILookup.
func (c *CacheResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	val, ok := c.cache.Get(name, qtype)
	if ok {
		return val, nil
	}

	key := fmt.Sprintf("%s:%d", name, qtype)
	res, err, _ := c.sfGroup.Do(key, func() (interface{}, error) {
		// Double check to see if another concurrent request already populated the cache.
		if val, ok := c.cache.Get(name, qtype); ok {
			return val, nil
		}
		ret, err := c.resolver.Lookup(ctx, name, qtype)
		if err != nil {
			return nil, err
		}
		c.cache.Set(name, qtype, ret)
		return ret, nil
	})

	if err != nil {
		return DNSRR{}, err
	}
	return res.(DNSRR), nil
}
