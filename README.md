# fastresolver

`fastresolver` is a composable DNS resolver library for Go. Version 3 returns
native `*dns.Msg` responses and keeps retry, cache, fallback, circuit-breaker,
CNAME-following, and adaptive upstream behavior behind the `ILookup` interface.

## Install

```bash
go get github.com/lixiangzhong/fastresolver/v3
```

The module supports Go 1.21 and later.

## Basic Lookup

```go
resolver, err := fastresolver.NewResolver("1.1.1.1")
if err != nil {
	return err
}

response, err := resolver.Lookup(ctx, "example.com", dns.TypeA)
if err != nil {
	return err
}
for _, record := range response.Answer {
	if address, ok := record.(*dns.A); ok {
		fmt.Println(address.A)
	}
}
```

## Default Resolver

`Default()` builds the bundled public resolvers as follows:

```text
per-upstream CircuitBreaker
-> AdaptivePoolResolver
-> CacheResolver
-> FollowCnameResolver
```

The adaptive pool starts conservatively and learns each upstream's usable QPS
from successes, timeouts, REFUSED, and SERVFAIL responses. It replaces the v2
fixed `100 QPS + RandomBalancer` default path.

```go
response, err := fastresolver.Default().Lookup(ctx, "example.com", dns.TypeAAAA)
```

## Adaptive Upstream Pool

No options are required:

```go
pool, err := fastresolver.NewAdaptivePoolResolver(upstreams)
```

The zero-configuration policy is:

```text
InitialQPS        = 10
MinQPS            = 1
MaxQPS            = 1000
IncreasePerSecond = 5
DecreaseFactor    = 0.7
FailureCooldown   = 100ms
MaxAttempts       = min(3, upstream count)
```

Override only the values needed by the deployment:

```go
pool, err := fastresolver.NewAdaptivePoolResolver(
	upstreams,
	fastresolver.WithAdaptiveInitialQPS(5),
	fastresolver.WithAdaptiveMaxQPS(500),
	fastresolver.WithAdaptiveIncreasePerSecond(10),
	fastresolver.WithAdaptiveDecreaseFactor(0.7),
	fastresolver.WithAdaptiveFailureCooldown(100*time.Millisecond),
	fastresolver.WithAdaptiveMaxAttempts(3),
)
```

`Stats()` returns the learned QPS, success/failure counts, and current in-flight
requests for each upstream in constructor order.

The adaptive controller treats transport errors, timeouts, REFUSED, and
SERVFAIL as failure feedback. Explicit caller cancellation does not reduce an
upstream's learned capacity. Retries use distinct upstreams.

## Response and Error Semantics

- NOERROR and NXDOMAIN return a response with a nil error.
- REFUSED returns the response with `ServerRefusedError`.
- SERVFAIL returns the response with `ServerFailureError`.
- Malformed or mismatched response questions return the response with a
  validation error.
- A response may therefore be non-nil when error is non-nil. Check error first.

## Cache

The built-in cache deep-copies messages at ownership boundaries, follows RFC
2308 for NXDOMAIN/NODATA, supports TTL bounds, and decrements record TTLs on
hits. Resolver failures, including SERVFAIL, are not written by `CacheResolver`.

## Validation

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

See [MIGRATION.md](MIGRATION.md) for the v2-to-v3 API migration.
