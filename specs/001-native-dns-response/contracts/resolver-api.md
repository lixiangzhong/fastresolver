# Resolver API Contract: v3 Native DNS Response

## Module Contract

The incompatible API is published under:

```text
github.com/lixiangzhong/fastresolver/v3
```

No v2-compatible return method, alias response model or automatic adapter is provided.

## Resolver Interface

```go
type ILookup interface {
	Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error)
}
```

The method name, receiver abstraction and three input parameters are unchanged. Every base resolver, decorator and user-provided resolver participates through this single interface.

## Outcome Invariants

1. A nil error requires a non-nil response.
2. A non-nil response may accompany an error when DNS data was obtained before a validation, fallback or policy failure.
3. Callers must inspect error before treating the response as successful.
4. RCODE is always read from `response.Rcode`.
5. Records remain in their protocol sections: `Answer`, `Ns` and `Extra`.
6. Transparent decorators do not modify a returned message.
7. Cached responses are independent deep copies and may be safely mutated by each caller.

## Error Matrix

| Condition | Response | Error |
|-----------|----------|-------|
| NOERROR, NXDOMAIN or other decoded non-failure RCODE | decoded message | nil |
| REFUSED | decoded message | `ServerRefusedError` |
| SERVFAIL | decoded message | `ServerFailureError` |
| Decoded response without Question | decoded/partial message | `ErrNoQuestion` |
| Response Question count/name/type/class mismatch | decoded/partial message | `ErrInvalidQuestion` |
| DoH response uses a non-zero message ID | decoded message | `ErrInvalidResponseID` |
| Resolver returned nil response and nil error | nil | `ErrNoResponse` |
| UDP truncated, TCP succeeds completely | TCP message | according to TCP RCODE |
| UDP truncated, TCP dial/exchange fails | truncated UDP message | TCP error |
| TCP response remains truncated | truncated TCP message | `TruncatedError` |
| Context canceled/deadline exceeded before response | nil | context error detectable with `errors.Is` |
| Circuit open | nil | `ErrCircuitBreaker` |
| No resolver available | nil | `ErrNoResolver` |
| CNAME depth exceeded | last response | error wrapping `ErrCnameDepthExceeded` |
| Recursive depth exceeded | last response when available | `ErrMaxRecursionDepth` |
| HTTP failure before DNS payload | nil | HTTP/transport error |
| JSON/wire decode failure | partial message when usable, otherwise nil | decode error |

## Cache Interface

```go
type Cache interface {
	Set(name string, qtype uint16, response *dns.Msg)
	Get(name string, qtype uint16) (*dns.Msg, bool)
}
```

Contract requirements:

- `Set` must not retain caller-owned mutable state.
- `Get` must not expose cache-owned mutable state.
- Nil values are not valid cache entries.
- The built-in LRU preserves the existing default/minimum TTL policy described in [data-model.md](../data-model.md).

Custom Cache implementations must adopt these ownership rules to be safely interchangeable with the built-in cache.

The built-in cache follows RFC 2308 negative TTL rules, decrements TTL on hits, and exposes configurable minimum/maximum bounds through `CacheOptions` and `NewLRUWithOptions`. `CacheResolver` does not store SERVFAIL because resolver errors bypass cache writes; direct low-level cache insertion still applies the 30-second SERVFAIL bound.

## Recursive Entry Point

```go
func RecursiveLookup(ctx context.Context, qname string, qtype uint16) (*dns.Msg, error)
```

`RecursiveResolver.Lookup` delegates to this function. Returned delegation records stay in `Msg.Ns`; the implementation does not copy them into Answer. A local public-zone miss returns a synthesized response with:

- `Response == true`
- original Question
- `Rcode == dns.RcodeNameError`

## Transport Mapping

### UDP/TCP

- Query name is sent as an FQDN.
- UDP is attempted first.
- TC triggers one TCP attempt against the same configured server.
- The returned message is the actual selected transport response, not a reconstructed subset.

### DoH

- Request remains `application/dns-message` over GET.
- A successful body is unpacked and returned directly.
- HTTP status/body metadata and elapsed time are not inserted into the DNS message.

### JSON DNS

| JSON source | `dns.Msg` target |
|-------------|------------------|
| Status | Rcode |
| TC | Truncated |
| RD | RecursionDesired |
| RA | RecursionAvailable |
| AD | AuthenticatedData |
| CD | CheckingDisabled |
| Question | Question with class IN |
| Answer | Answer |
| Authority | Ns |

`Response` is true. Source-unavailable Id, AA, Opcode and Extra remain zero/empty. Comment is discarded. RR type parsing uses the DNS library's standard parser and fails closed for invalid or newline-bearing external text.

## Decorator Contract

| Decorator | Response behavior |
|-----------|-------------------|
| Retry | Returns first nil-error response or final attempt outcome |
| Timeout | Preserves underlying response; returns context-detectable timeout/cancel errors |
| RateLimit | Waits for capacity with context cancellation, then passes outcome unchanged |
| Metrics | Counts non-nil error as failure, nil error as success |
| CircuitBreaker | Uses error for transitions and passes underlying outcome unchanged |
| LoadBalance | Delegates to selected resolver; no resolver yields `ErrNoResolver` |
| Fallback | On primary error, returns secondary outcome |
| Concurrency | First non-nil nil-error response wins; all failures join errors and may retain one failed response |
| Cache | Caches only nil-error non-nil responses and returns isolated copies |
| FollowCname | May issue subsequent lookup; returns final response or current response with depth error |

## Removed API and Field Migration

| Removed API | Native replacement |
|-------------|--------------------|
| `DNSRR.Rcode` | `msg.Rcode` |
| `DNSRR.NXDomain` | `msg.Rcode == dns.RcodeNameError` |
| `DNSRR.Authoritative` | `msg.Authoritative` |
| `DNSRR.A`, `AAAA`, `NS`, `CNAME`, `PTR`, `MX`, `TXT`, `SRV`, `SOA` | Type assertions over `msg.Answer` or `msg.Ns` |
| `DNSRR.AuthNS` | `*dns.NS` records in `msg.Ns` |
| `DNSRR.TTL` | Individual `rr.Header().Ttl` values |
| `DNSRR.ServerAddr`, `Network`, `Rtt` | No replacement in this feature |
| fastresolver `MX`, `SOA`, `AuthNS` | `dns.MX`, `dns.SOA`, `dns.NS` |

NOERROR with empty Answer and SOA authority is represented as NODATA, not rewritten to NXDOMAIN. Existing internal recursive termination may retain its legacy SOA heuristic without changing the message.

## Adaptive Pool Contract

`NewAdaptivePoolResolver(resolvers, opts...)` uses defaults when no functional options are supplied. It retries distinct upstreams, increases learned QPS after success, and reduces learned QPS after timeout, transport error, REFUSED, or SERVFAIL. Explicit caller cancellation does not penalize an upstream. `Stats()` returns learned QPS, success/failure counters, and in-flight requests in constructor order.

`Default()` uses this adaptive pool instead of the v2 fixed-rate random pool.
