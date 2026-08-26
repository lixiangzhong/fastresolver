# Data Model: Native DNS Response

## Overview

This feature adds no persistent data. It replaces one in-memory result model with the DNS library's native message and defines ownership, validation and state transitions around that mutable object.

## Entity: Lookup Request

| Field | Type | Rules |
|-------|------|-------|
| Context | `context.Context` | Cancellation and deadlines propagate through every resolver layer |
| Name | string | Converted to the canonical DNS query name by wire-based resolvers |
| Query type | uint16 | Passed unchanged; may be a known or numeric DNS RR type |

**Relationships**: A request enters one `ILookup`; decorators may retry, cache, race or redirect it, but every attempt produces the same Lookup Outcome shape.

## Entity: Native DNS Response

**Type**: `*dns.Msg`

| Field group | Source | Validation/usage |
|-------------|--------|------------------|
| MsgHdr | Wire response or JSON flag mapping | Rcode and protocol flags remain unmodified |
| Question | Wire/JSON response | At least one entry is required for a successful library response |
| Answer | Wire/JSON response | Returned unchanged; inspected for requested RR, A and CNAME |
| Authority (`Ns`) | Wire/JSON response | Returned unchanged; inspected for delegation NS, SOA and cache TTL |
| Additional (`Extra`) | Wire response only | Returned unchanged; not used by current recursive routing or cache TTL |

**Validation rules**:

- `nil` cannot represent a successful response.
- Missing Question produces `ErrNoQuestion` while preserving the non-nil message.
- REFUSED preserves the message and produces `ServerRefusedError`.
- REFUSED and SERVFAIL produce typed Go errors while retaining the response; other RCODE values remain protocol data.
- JSON-created RR values must have valid names, no CR/LF injection, IN class, and source-matching type and TTL.

## Entity: Lookup Outcome

| State | Response | Error | Downstream behavior |
|-------|----------|-------|---------------------|
| Success | non-nil | nil | May be cached and returned; retry/breaker treat as success |
| DNS policy failure | non-nil | typed/non-nil | May be inspected; not cached; retry/fallback/breaker treat as failure |
| Local/transport failure | nil | non-nil | No response to inspect; retry/fallback may continue |
| Partial failure | non-nil | non-nil | Preserve available response; retry/fallback may replace it with a later outcome |
| Invalid implementation | nil | nil | Normalize to `ErrNoResponse` |

### State Transitions

```text
request
  -> local policy rejection ------------------------> local/transport failure
  -> transport/decode failure without message ------> local/transport failure
  -> decoded message
       -> missing Question --------------------------> partial failure
       -> REFUSED -----------------------------------> DNS policy failure
       -> TC over UDP
            -> TCP complete -------------------------> success/DNS policy failure
            -> TCP failure --------------------------> partial failure with UDP message
            -> TCP still truncated -----------------> partial failure with TCP message
       -> SERVFAIL -----------------------------------> DNS policy failure
       -> any other RCODE ----------------------------> success
```

## Entity: Cached Response

| Field | Type | Rules |
|-------|------|-------|
| Key name | string | Existing exact name key behavior remains |
| Key type | uint16 | Existing qtype partition remains |
| Value | `*dns.Msg` | Stored as a deep copy; never nil |
| Expiry | `time.Time` | Derived at Set time from existing TTL policy |

**Ownership rules**:

- Set copies caller-owned input before storage.
- Get copies storage-owned input before return.
- Singleflight copies its shared result for every caller.
- Callers may mutate a returned message without changing the cache or another caller's response.

**TTL rules**:

1. Find the minimum Header TTL in Answer and Authority.
2. If no RR exists or the minimum is zero, use the configured default TTL.
3. Raise any final TTL below one minute to one minute.
4. Extra does not participate and RR TTL values are not decremented on hits.

## Entity: JSON DNS Transport DTO

The existing `JSONAPIResponse`, `JSONAPIQuestion(s)` and `JSONAPIAnswer` remain transport-only DTOs. They are not lookup result alternatives.

| DTO field | Native response target |
|-----------|------------------------|
| Status | `MsgHdr.Rcode` |
| TC/RD/RA/AD/CD | Corresponding MsgHdr flags |
| Question | `Msg.Question`, class IN |
| Answer | `Msg.Answer` |
| Authority | `Msg.Ns` |
| Comment | No target; intentionally discarded |
| Error | Go error; constructed partial message is preserved |

Fields not represented by the source remain zero/empty. Additional records cannot be created because the DTO has no Additional field.

## Entity: Resolver Decorator

Transparent decorators keep an `ILookup` relationship to the next resolver. They must not mutate `dns.Msg`:

- Retry returns the last attempt outcome.
- Timeout changes context only.
- Rate limit delays forwarding only.
- Metrics and circuit breaker classify by error.
- Load balance selects one resolver.
- Fallback returns the secondary outcome after a primary error.
- Concurrency preserves response/error pairing per attempt.
- Cache is the sole decorator that copies successful messages for ownership isolation.

## Removed Entities

- `DNSRR`
- `AuthNS`
- fastresolver `MX`
- fastresolver `SOA`
- Aggregated `TTL`, `NXDomain`, transport Network, ServerAddr and Rtt fields

Their DNS protocol equivalents are read from `dns.Msg` and concrete `dns.RR` values. Transport metadata has no replacement in this feature.
