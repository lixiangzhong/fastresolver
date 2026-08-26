# Migrating from v2 to v3

Version 3 replaces the aggregated `fastresolver.DNSRR` lookup result with the native response type from `github.com/miekg/dns`.

## Import Path

Update imports:

```go
import "github.com/lixiangzhong/fastresolver/v3"
```

The query method keeps its name and inputs, but changes its result:

```go
type ILookup interface {
	Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error)
}
```

Custom resolver implementations and custom `Cache` implementations must adopt `*dns.Msg` as well.

## Reading Results

DNS records retain their protocol sections and concrete miekg/dns types:

```go
response, err := resolver.Lookup(ctx, "example.com", dns.TypeA)
if err != nil {
	// response may still contain a DNS message; inspect it only after handling err.
	return err
}

for _, record := range response.Answer {
	if address, ok := record.(*dns.A); ok {
		fmt.Println(address.A)
	}
}
```

| v2 field/type | v3 replacement |
|---------------|----------------|
| `DNSRR.Rcode` | `response.Rcode` |
| `DNSRR.NXDomain` | `response.Rcode == dns.RcodeNameError` |
| `DNSRR.Authoritative` | `response.Authoritative` |
| `DNSRR.A`, `AAAA`, `NS`, `CNAME`, `PTR`, `MX`, `TXT`, `SRV`, `SOA` | Type assertions over `response.Answer` or `response.Ns` |
| `DNSRR.AuthNS` | `*dns.NS` values in `response.Ns` |
| `DNSRR.TTL` | Each record's `Header().Ttl` |
| fastresolver `AuthNS`, `MX`, `SOA` | `dns.NS`, `dns.MX`, `dns.SOA` |

`DNSRR`, `AuthNS`, fastresolver `MX`, fastresolver `SOA`, and their conversion helpers have been removed. No compatibility alias or parallel lookup API is provided.

## Removed Transport Metadata

`ServerAddr`, `Network`, and `Rtt` are not DNS protocol fields and are no longer returned. Version 3 does not introduce a wrapper or side-channel replacement for them.

## Response and Error Contract

- A nil error always has a non-nil response.
- A non-nil response may accompany an error when DNS data was obtained before validation, fallback, or policy failure.
- NXDOMAIN and decoded RCODE values other than REFUSED/SERVFAIL return the message with a nil Go error.
- REFUSED returns the message with `ServerRefusedError`.
- SERVFAIL returns the message with `ServerFailureError`, so retry, circuit-breaker, fallback, and adaptive feedback all treat it as failure.
- Missing Question returns the partial message with `ErrNoQuestion`.
- A custom resolver returning `nil, nil` is normalized to `ErrNoResponse` by resolver layers that consume it.
- A failed TCP fallback returns the truncated UDP message with the TCP error.

Callers must check `error` before consuming a non-nil response.

## NODATA and NXDOMAIN

Version 2 could infer `NXDomain` from an empty Answer plus an SOA Authority record even when the upstream RCODE was NOERROR. Version 3 exposes the native RCODE:

- `RcodeNameError` is NXDOMAIN.
- `RcodeSuccess` with an empty Answer is NODATA, even when Authority contains SOA.

The recursive resolver retains its previous SOA heuristic only for internal termination decisions; it does not rewrite the returned message.

## Cache Ownership

The public cache contract is now:

```go
type Cache interface {
	Set(name string, qtype uint16, response *dns.Msg)
	Get(name string, qtype uint16) (*dns.Msg, bool)
}
```

Custom caches must isolate mutable messages. The built-in LRU deep-copies on Set, Get, and singleflight delivery so callers may safely mutate returned responses.

The built-in cache now also:

- follows RFC 2308 for NXDOMAIN and NODATA using `min(SOA TTL, SOA.MINIMUM)`;
- does not cache SERVFAIL through `CacheResolver` because it is a resolver error;
- refuses truncated, malformed, zero-TTL, and non-cacheable responses;
- returns remaining TTL values on cache hits instead of the original values;
- supports optional TTL bounds through `NewLRUWithOptions` and `CacheOptions`.

`NewLRU(size, ttl)` remains available. Its `ttl` argument is the fallback TTL for cacheable responses without a usable record TTL; it no longer imposes an unconditional one-minute minimum.

## Default and Adaptive Upstreams

Version 2 used a fixed 100 QPS limiter on every bundled upstream followed by random selection. Version 3 `Default()` uses `AdaptivePoolResolver` instead, while retaining per-upstream circuit breakers and the outer cache/CNAME layers.

The adaptive pool needs no explicit options:

```go
pool, err := fastresolver.NewAdaptivePoolResolver(upstreams)
```

Deployments can override individual defaults with `WithAdaptiveInitialQPS`, `WithAdaptiveMinQPS`, `WithAdaptiveMaxQPS`, `WithAdaptiveIncreasePerSecond`, `WithAdaptiveDecreaseFactor`, `WithAdaptiveFailureCooldown`, and `WithAdaptiveMaxAttempts`.

Timeouts, transport errors, REFUSED, and SERVFAIL reduce learned capacity. Explicit caller cancellation releases the request without penalizing the upstream. Fixed `RateLimitResolver` remains available and now stops waiting when its context is canceled or expires.
