# Research: Native DNS Response

## Decision 1: Use `*dns.Msg` as the only lookup response

**Decision**: Use `github.com/miekg/dns` v1.1.73 and change the common lookup contract to `Lookup(context.Context, string, uint16) (*dns.Msg, error)`. Use `github.com/zonedb/zonedb` v1.0.5780 for recursive zone data. Remove the custom `DNSRR` family and conversion functions.

**Rationale**: The dependency already returns `*dns.Msg` for wire queries. Direct exposure preserves Question, Answer, Authority, Additional, RCODE, flags, EDNS and future RR types without maintaining a lossy parallel model.

**Alternatives considered**: A new response wrapper or alias and parallel old/new methods were rejected because they recreate a second model or compatibility path.

## Decision 2: Release as Go module v3

**Decision**: Change the module path and tracked self-imports from `/v2` to `/v3`; document a v3 migration. Do not tag or commit during implementation.

**Rationale**: Changing an interface method's return type breaks all callers and custom implementations. Go semantic import versioning requires a new major module path for a publishable incompatible release.

**Alternatives considered**: Publishing under `/v2` violates its compatibility promise. Keeping `/v2` temporarily is suitable only for a prototype, not the specified new major release.

## Decision 3: Define response/error combinations explicitly

**Decision**:

| Response | Error | Meaning |
|----------|-------|---------|
| non-nil | nil | Completed DNS response; caller may consume it |
| non-nil | non-nil | A response exists, but fallback, validation, or resolver policy failed |
| nil | non-nil | No usable DNS response exists |
| nil | nil | Invalid resolver result; normalize to `ErrNoResponse` |

NXDOMAIN and non-failure RCODEs use `(msg, nil)`. REFUSED uses `(msg, ServerRefusedError)` and SERVFAIL uses `(msg, ServerFailureError)`. Decorators classify both explicit DNS failure codes through the error path.

**Rationale**: This preserves existing retry, cache, fallback and circuit-breaker behavior while retaining already available responses and preventing unsafe empty successes.

**Alternatives considered**: Treating every non-success RCODE as an error would incorrectly include NXDOMAIN; discarding responses on error violates FR-008; allowing `nil, nil` creates nil dereference and false-success risks.

## Decision 4: Preserve UDP-to-TCP fallback and partial responses

**Decision**: Keep automatic TCP retry after a truncated UDP response. Return only the TCP message on success. If TCP fails, return the truncated UDP message with the TCP error. If TCP also returns TC, return that TCP message with `TruncatedError`.

**Rationale**: `miekg/dns.Client` does not perform this fallback automatically. The chosen contract preserves useful state without presenting incomplete data as successful.

**Alternatives considered**: Dropping the UDP response loses diagnostics; returning a truncated response with nil error misrepresents completeness.

## Decision 5: Deep-copy cached messages at every ownership boundary

**Decision**: `Cache.Set` stores `msg.Copy()`, `Cache.Get` returns a new `Copy()`, and `CacheResolver` copies the singleflight result separately for every waiter. Nil messages are never cached.

**Rationale**: `dns.Msg`, its slices and RR values are mutable. `Msg.Copy()` deep-copies every section and prevents the cache or concurrent callers from sharing mutable state.

**Alternatives considered**: Copying only on Set or Get leaves a mutation path. Read-only convention cannot be enforced. Packed wire caching adds cost and error paths.

## Decision 6: Preserve current cache TTL policy

**Decision**: Compute the minimum RR Header TTL across Answer and Authority (`Msg.Ns`). If no TTL exists or the minimum is zero, use the configured default. Preserve the one-minute minimum. Do not scan Extra and do not decrement returned TTLs on cache hits.

**Rationale**: This replaces the former aggregation without changing observable cache expiry behavior and avoids treating OPT pseudo-record fields as cache TTL.

**Alternatives considered**: RFC 2308 negative-cache TTL, Additional-section TTL, hit-time TTL decrement and removing the minimum are valid separate changes but not behavior-preserving migration choices.

## Decision 7: Map JSON DNS with the DNS library parser

**Decision**: Construct a `dns.Msg` with `Response=true`; map Status/TC/RD/RA/AD/CD to MsgHdr, JSON Question to IN-class questions, Answer to `Msg.Answer`, and Authority to `Msg.Ns`. Parse each RDATA with `dns.NewRR` using a fixed safe owner, then assign a separately validated source owner and verify type/class/TTL. Reject CR/LF in external name/data. A missing Question returns the partial message with `ErrNoQuestion`.

**Rationale**: The library parser supports its full RR vocabulary, including types that the current manual switch loses. Fixed-owner parsing prevents untrusted JSON owner text from injecting zone directives or extra records.

**Alternatives considered**: Manual construction duplicates DNS parsing and omits types. Filling missing fields from the request fabricates upstream data. Encoding Comment or HTTP metadata in DNS records corrupts protocol semantics.

JSON fields without a DNS equivalent remain absent: Id 0, Opcode QUERY, Authoritative false and Extra empty; Comment is discarded. The request type uses `dns.Type(qtype).String()` so unknown numeric types are not sent as an empty string.

## Decision 8: Return DoH wire messages directly

**Decision**: Unpack `application/dns-message` into `*dns.Msg` and return it directly after common Question/REFUSED validation. Remove round-trip timing collection because it has no place in the selected response contract.

**Rationale**: Rebuilding a message after successful wire decoding can only lose fields, RR types or EDNS options.

**Alternatives considered**: Selected-field reconstruction and a hidden DNSRR conversion path were rejected as lossy duplicate models.

## Decision 9: Keep decorator policy, repair result pairing

**Decision**: Retry, timeout, rate-limit, metrics, circuit-breaker, load-balance and fallback pass response pointers unchanged and continue using error as the policy signal. Concurrency uses one result channel carrying response and error together; the first non-nil successful response wins, zero resolvers return `ErrNoResolver`, and all-failure results join errors while retaining one associated response.

**Rationale**: Separate response/error channels cannot represent `(msg, err)` and can read closed-channel zero values as false successes.

**Alternatives considered**: Keeping two channels loses association. Discarding all failed responses violates the new contract. A child resolver's `nil, nil` result is normalized to `ErrNoResponse`.

## Decision 10: Read CNAME and recursive state from DNS sections

**Decision**: CNAME following scans only Answer, follows the first CNAME only for A/AAAA/PTR when the requested record type is absent, and returns only the final query response. Recursive lookup reads A from Answer, delegation NS from Authority, and terminal state from MsgHdr. Preserve the existing SOA-based negative heuristic only for internal recursive control flow; never overwrite upstream RCODE or move records between sections.

**Rationale**: These mappings retain current algorithms while returning the unmodified native message. A local zonedb miss becomes a synthesized response with the original Question and NXDOMAIN RCODE.

**Alternatives considered**: Merging CNAME messages, moving Authority NS to Answer, using Extra glue or rewriting NODATA to NXDOMAIN would change or fabricate protocol behavior.

## Decision 11: Keep validation deterministic and document migration

**Decision**: Use local UDP and HTTP/DoH fixtures for response fidelity, RCODEs, fallback, malformed input, cache isolation and wrapper composition. Add root `MIGRATION.md`. Migrate the tracked executable example test; do not modify ignored local prototype files.

**Rationale**: The repository already has deterministic fixtures. A dedicated migration document is needed because no tracked README/CHANGELOG exists and GoDoc cannot explain all v2-to-v3 mappings.

**Alternatives considered**: Public DNS tests are nondeterministic. Code comments and AGENTS.md are not adequate user-facing migration documentation.

## Baseline Finding

`go test ./...` passes before implementation. `go vet ./...` is currently blocked only by unreachable code in the ignored local file `example/main.go`; that file is not part of the versioned repository. Validation of the delivered change must run against the tracked tree and report this local-workspace distinction explicitly.
