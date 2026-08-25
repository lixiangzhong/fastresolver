package fastresolver

import (
	"context"
	"fmt"
	"math"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/miekg/dns"
	"golang.org/x/sync/singleflight"
)

// DefaultMemCache 是默认的内存缓存实例（容量 50000，默认 TTL 1 分钟）。
var DefaultMemCache = NewLRU(50000, time.Minute)

// DefalutMemCache 是由于早期拼写错误保留的别名，现已弃用，请优先使用 DefaultMemCache。
// Deprecated: 使用 DefaultMemCache 代替。
var DefalutMemCache = DefaultMemCache

type Cache interface {
	Set(name string, qtype uint16, response *dns.Msg)
	Get(name string, qtype uint16) (*dns.Msg, bool)
}

type cacheKey struct {
	name  string
	qtype uint16
}

type cacheItem struct {
	value     *dns.Msg
	expiredAt time.Time
}

// CacheOptions controls the built-in LRU cache TTL policy.
type CacheOptions struct {
	// DefaultTTL is used for cacheable responses that don't carry a usable TTL.
	DefaultTTL time.Duration

	// MinTTL raises shorter cache lifetimes.  Zero disables the override.
	MinTTL time.Duration

	// MaxTTL lowers longer cache lifetimes.  Zero disables the override.
	MaxTTL time.Duration
}

var _ Cache = (*memLRU)(nil)

type memLRU struct {
	cache   *lru.Cache[cacheKey, cacheItem]
	options CacheOptions
	now     func() time.Time
}

func NewLRU(size int, ttl time.Duration) Cache {
	cache, err := NewLRUWithOptions(size, CacheOptions{DefaultTTL: ttl})
	if err != nil {
		panic(err)
	}
	return cache
}

// NewLRUWithOptions creates an LRU cache with configurable TTL bounds.
func NewLRUWithOptions(size int, options CacheOptions) (Cache, error) {
	if options.DefaultTTL < 0 || options.MinTTL < 0 || options.MaxTTL < 0 {
		return nil, fmt.Errorf("cache TTL values must not be negative")
	}
	if options.MaxTTL > 0 && options.MinTTL > options.MaxTTL {
		return nil, fmt.Errorf("cache minimum TTL %s exceeds maximum TTL %s", options.MinTTL, options.MaxTTL)
	}
	store, err := lru.New[cacheKey, cacheItem](size)
	if err != nil {
		return nil, err
	}
	return &memLRU{cache: store, options: options, now: time.Now}, nil
}

// Get implements Cache.
func (cache *memLRU) Get(name string, qtype uint16) (*dns.Msg, bool) {
	key := cacheKey{name: name, qtype: qtype}
	item, ok := cache.cache.Get(key)
	if !ok {
		return nil, false
	}
	now := cache.now()
	if !now.Before(item.expiredAt) {
		cache.cache.Remove(key)
		return nil, false
	}
	response := item.value.Copy()
	setResponseTTL(response, durationToTTL(item.expiredAt.Sub(now)))
	return response, true
}

// Set implements Cache.
func (cache *memLRU) Set(name string, qtype uint16, response *dns.Msg) {
	if response == nil {
		return
	}
	ttl, ok := cacheTTL(response, cache.options.DefaultTTL)
	if !ok {
		return
	}
	ttl = respectTTLOverrides(ttl, cache.options.MinTTL, cache.options.MaxTTL)
	if response.Rcode == dns.RcodeServerFailure && ttl > servFailMaxCacheTTL {
		ttl = servFailMaxCacheTTL
	}
	cache.cache.Add(cacheKey{name: name, qtype: qtype}, cacheItem{
		value:     response.Copy(),
		expiredAt: cache.now().Add(ttl),
	})
}

// servFailMaxCacheTTL follows the RFC 2308 upper bound for transient failures.
const servFailMaxCacheTTL = 30 * time.Second

// cacheTTL returns the effective lifetime of a cacheable response.
func cacheTTL(response *dns.Msg, defaultTTL time.Duration) (time.Duration, bool) {
	if response == nil || response.Truncated || len(response.Question) != 1 {
		return 0, false
	}

	switch response.Rcode {
	case dns.RcodeSuccess:
		if len(response.Answer) == 0 {
			if ttl, ok := negativeResponseTTL(response); ok {
				return ttl, ttl > 0
			}
		}
		qtype := response.Question[0].Qtype
		if (qtype == dns.TypeA || qtype == dns.TypeAAAA) && !hasAddressAnswer(response, qtype) {
			return 0, false
		}
		ttl := minimumResponseTTL(response)
		return ttl, ttl > 0
	case dns.RcodeNameError:
		ttl, ok := negativeResponseTTL(response)
		return ttl, ok && ttl > 0
	case dns.RcodeServerFailure:
		ttl := minimumResponseTTL(response)
		if ttl == 0 {
			ttl = defaultTTL
		}
		if ttl > servFailMaxCacheTTL {
			ttl = servFailMaxCacheTTL
		}
		return ttl, ttl > 0
	default:
		return 0, false
	}
}

// negativeResponseTTL applies RFC 2308 to NXDOMAIN and NODATA responses.
func negativeResponseTTL(response *dns.Msg) (time.Duration, bool) {
	var minimum uint32 = math.MaxUint32
	foundSOA := false
	for _, record := range response.Ns {
		switch record := record.(type) {
		case *dns.NS:
			return 0, false
		case *dns.SOA:
			foundSOA = true
			ttl := min(record.Hdr.Ttl, record.Minttl)
			if ttl < minimum {
				minimum = ttl
			}
		}
	}
	if !foundSOA || minimum == 0 {
		return 0, foundSOA
	}
	return time.Duration(minimum) * time.Second, true
}

// minimumResponseTTL returns the lowest non-OPT TTL across all response sections.
func minimumResponseTTL(response *dns.Msg) time.Duration {
	minimum := uint32(math.MaxUint32)
	for _, section := range [][]dns.RR{response.Answer, response.Ns, response.Extra} {
		for _, record := range section {
			header := record.Header()
			if header.Rrtype != dns.TypeOPT && header.Ttl < minimum {
				minimum = header.Ttl
			}
		}
	}
	if minimum == math.MaxUint32 || minimum == 0 {
		return 0
	}
	return time.Duration(minimum) * time.Second
}

// hasAddressAnswer reports whether Answer contains the requested address type.
func hasAddressAnswer(response *dns.Msg, qtype uint16) bool {
	for _, record := range response.Answer {
		switch record.(type) {
		case *dns.A:
			if qtype == dns.TypeA {
				return true
			}
		case *dns.AAAA:
			if qtype == dns.TypeAAAA {
				return true
			}
		}
	}
	return false
}

// respectTTLOverrides clamps ttl to the configured bounds.
func respectTTLOverrides(ttl, minimum, maximum time.Duration) time.Duration {
	if minimum > 0 && ttl < minimum {
		ttl = minimum
	}
	if maximum > 0 && ttl > maximum {
		ttl = maximum
	}
	return ttl
}

// durationToTTL rounds a positive duration up to whole DNS TTL seconds.
func durationToTTL(duration time.Duration) uint32 {
	seconds := (duration + time.Second - 1) / time.Second
	if seconds > time.Duration(math.MaxUint32) {
		return math.MaxUint32
	}
	return uint32(seconds)
}

// setResponseTTL updates all cacheable records while preserving OPT flags.
func setResponseTTL(response *dns.Msg, ttl uint32) {
	for _, section := range [][]dns.RR{response.Answer, response.Ns, response.Extra} {
		for _, record := range section {
			if header := record.Header(); header.Rrtype != dns.TypeOPT {
				header.Ttl = ttl
			}
		}
	}
}

var _ ILookup = (*CacheResolver)(nil)

type CacheResolver struct {
	cache    Cache
	resolver ILookup
	sfGroup  singleflight.Group
}

func NewCacheResolver(cache Cache, resolver ILookup) *CacheResolver {
	return &CacheResolver{cache: cache, resolver: resolver}
}

// Lookup implements ILookup.
func (resolver *CacheResolver) Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	if response, ok := resolver.cache.Get(name, qtype); ok {
		return normalizeLookupResult(response, nil)
	}

	key := fmt.Sprintf("%s:%d", name, qtype)
	result := resolver.sfGroup.DoChan(key, func() (interface{}, error) {
		if response, ok := resolver.cache.Get(name, qtype); ok {
			return response, nil
		}
		response, err := normalizeLookupResult(resolver.resolver.Lookup(ctx, name, qtype))
		if err != nil {
			return response, err
		}
		resolver.cache.Set(name, qtype, response)
		return response, nil
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resolved := <-result:
		response, _ := resolved.Val.(*dns.Msg)
		if response != nil {
			response = response.Copy()
		}
		return normalizeLookupResult(response, resolved.Err)
	}
}

// Unwrap returns the underlying resolver.
func (resolver *CacheResolver) Unwrap() ILookup {
	return resolver.resolver
}
