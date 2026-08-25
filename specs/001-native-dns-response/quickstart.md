# Quickstart: Validate Native DNS Response

## Prerequisites

- Go 1.21 or later within the module's declared compatibility range
- Checkout of branch `refactor/native-dns-response`
- No external DNS or database service; all protocol tests use local fixtures

The repository may contain ignored local prototypes under `example/`. For release validation, use a clean tracked checkout so ignored files cannot affect `go vet ./...`, or resolve the already reported local unreachable-code warning separately.

## 1. Verify the public contract

```bash
go doc github.com/lixiangzhong/fastresolver/v3.ILookup
go doc github.com/lixiangzhong/fastresolver/v3.Cache
go doc github.com/lixiangzhong/fastresolver/v3.RecursiveLookup
```

Expected:

- `Lookup` and `RecursiveLookup` return `*dns.Msg, error`.
- `Cache` stores and retrieves `*dns.Msg`.
- No public `DNSRR`, `AuthNS`, fastresolver `MX`, or fastresolver `SOA` remains.

Static removal check:

```bash
rg -n 'type (DNSRR|AuthNS|MX|SOA)\b|\btoDNSRR\b|\bnewSOA\b' --glob '*.go'
```

Expected: no matches.

## 2. Validate native message fidelity

```bash
go test -run 'TestResolver_.*Native|TestDoH_.*Native|TestJSONAPI_.*Native' ./...
```

Expected:

- UDP/TCP and DoH retain Question, Answer, Authority, Additional, RCODE and flags.
- JSON DNS maps every source field it can express and reports invalid/missing fields.
- NXDOMAIN/SERVFAIL return a message without Go error; REFUSED returns both message and typed error.

## 3. Validate fallback and error boundaries

```bash
go test -run 'TestResolver_.*Truncated|TestResolver_.*NoQuestion|Test.*NoResponse|Test.*Context' ./...
```

Expected:

- UDP TC falls back to complete TCP.
- Failed TCP fallback retains the truncated UDP message with an error.
- Empty Question and `nil, nil` custom implementations become explicit errors.
- Context cancellation/deadline errors remain detectable with `errors.Is`.

## 4. Validate cache ownership and concurrency

```bash
go test -run 'TestCacheResolver_.*(Copy|Isolation|Singleflight)|TestConcurrencyResolver_' ./...
go test -race -run 'TestCacheResolver_.*Singleflight|TestConcurrencyResolver_' ./...
```

Expected:

- A caller cannot mutate stored cache state or another caller's response.
- Concurrent cache misses invoke the underlying resolver once and return distinct messages.
- Concurrency preserves each response/error pair, returns `ErrNoResolver` for an empty set, and never reports `nil, nil` success.

## 5. Validate inspecting resolvers

```bash
go test -run 'TestFollowCnameResolver_|TestRecursiveLookup' ./...
```

Expected:

- CNAME following reads Answer records and preserves its existing type/depth policy.
- Recursive lookup reads A and Authority NS records from native sections.
- Returned authority records stay in `Msg.Ns`, and native RCODE is not rewritten.
- Recursive cache hits return independent response copies.

## 6. Run complete quality gates

From a clean tracked checkout:

```bash
gofmt -w <modified-go-files>
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

Expected: every command exits successfully without public-network dependency.

## 7. Review migration coverage

Read `MIGRATION.md` and verify it covers:

- `/v2` to `/v3` import change
- `ILookup` and custom cache implementation migration
- Answer/Authority/Additional and RCODE access
- removal of aggregate TTL/NXDomain and transport metadata
- response-with-error behavior
- NODATA versus NXDOMAIN behavior

The authoritative interface details are in [contracts/resolver-api.md](contracts/resolver-api.md), with ownership and state rules in [data-model.md](data-model.md).
