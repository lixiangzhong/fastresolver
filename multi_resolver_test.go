package fastresolver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

const (
	mockUpstreamCount       = 16
	mockUpstreamQPS         = 20
	requestsPerMockUpstream = 41
)

type mockRateLimitedUpstream struct {
	address    string
	calls      atomic.Uint64
	err        error
	mu         sync.Mutex
	receivedAt []time.Time
}

func (upstream *mockRateLimitedUpstream) Lookup(
	ctx context.Context,
	name string,
	qtype uint16,
) (*dns.Msg, error) {
	upstream.calls.Add(1)
	upstream.mu.Lock()
	upstream.receivedAt = append(upstream.receivedAt, time.Now())
	upstream.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if upstream.err != nil {
		return nil, upstream.err
	}

	response := newTestResponse(name, qtype)
	response.Answer = []dns.RR{&dns.A{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(name),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    60,
		},
		A: net.ParseIP(upstream.address),
	}}
	return response, nil
}

func (upstream *mockRateLimitedUpstream) requestTimes() []time.Time {
	upstream.mu.Lock()
	defer upstream.mu.Unlock()

	return append([]time.Time(nil), upstream.receivedAt...)
}

func TestLoadBalanceResolver_UsesAggregatePerUpstreamRate(t *testing.T) {
	upstreams, resolvers := newRateLimitedUpstreams(nil)
	var resolver ILookup = NewLoadBalanceResolver(NewRoundRobinBalancer(), resolvers...)

	totalRequests := mockUpstreamCount * requestsPerMockUpstream
	start := make(chan struct{})
	errCh := make(chan error, totalRequests)
	var waitGroup sync.WaitGroup
	waitGroup.Add(totalRequests)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	startedAt := time.Now()
	for requestIndex := 0; requestIndex < totalRequests; requestIndex++ {
		go func() {
			defer waitGroup.Done()
			<-start
			response, err := resolver.Lookup(ctx, "aggregate.example", dns.TypeA)
			if err != nil {
				errCh <- err
				return
			}
			if response == nil || len(response.Answer) != 1 {
				errCh <- errors.New("resolver returned an invalid response")
			}
		}()
	}
	close(start)
	waitGroup.Wait()
	elapsed := time.Since(startedAt)
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}
	for upstreamIndex, upstream := range upstreams {
		if calls := upstream.calls.Load(); calls != requestsPerMockUpstream {
			t.Fatalf(
				"upstream %d received %d requests, want %d",
				upstreamIndex,
				calls,
				requestsPerMockUpstream,
			)
		}
	}

	aggregateQPS := float64(totalRequests) / elapsed.Seconds()
	minimumAggregateQPS := float64(mockUpstreamCount*mockUpstreamQPS) * 0.8
	t.Logf(
		"served %d requests through %d upstreams in %s (%.1f qps, nominal capacity %d qps)",
		totalRequests,
		mockUpstreamCount,
		elapsed,
		aggregateQPS,
		mockUpstreamCount*mockUpstreamQPS,
	)
	if aggregateQPS < minimumAggregateQPS {
		t.Fatalf(
			"aggregate rate %.1f qps is below expected minimum %.1f qps; elapsed=%s",
			aggregateQPS,
			minimumAggregateQPS,
			elapsed,
		)
	}
	if elapsed < 500*time.Millisecond {
		t.Fatalf("per-upstream rate limits were not applied; elapsed=%s", elapsed)
	}
}

func TestLegacyRandomPool_UsesMostEqualUpstreamCapacity(t *testing.T) {
	qpsByUpstream := []int{100, 100, 100, 100, 100, 100, 100, 100, 100, 100}
	result := runLegacyRandomPoolLoad(t, qpsByUpstream, 200, 1000)

	minimumQPS := float64(result.nominalQPS) * 0.7
	if result.actualQPS < minimumQPS {
		t.Fatalf("default-style resolver served %.1f qps, want at least %.1f qps", result.actualQPS, minimumQPS)
	}
}

func TestLegacyRandomPool_UnequalRatesAreQoSSafeButUnderutilized(t *testing.T) {
	qpsByUpstream := []int{50, 60, 70, 80, 90, 100, 110, 120, 130, 140}
	result := runLegacyRandomPoolLoad(t, qpsByUpstream, 200, 950)

	if result.actualQPS >= float64(result.nominalQPS)*0.75 {
		t.Fatalf(
			"random balancing unexpectedly served %.1f qps of %d nominal qps",
			result.actualQPS,
			result.nominalQPS,
		)
	}
}

type legacyRandomPoolLoadResult struct {
	actualQPS  float64
	nominalQPS int
}

func runLegacyRandomPoolLoad(
	t *testing.T,
	qpsByUpstream []int,
	concurrency int,
	totalRequests int,
) legacyRandomPoolLoadResult {
	t.Helper()

	upstreams := make([]*mockRateLimitedUpstream, 0, len(qpsByUpstream))
	resolvers := make([]ILookup, 0, len(qpsByUpstream))
	nominalQPS := 0
	for upstreamIndex, qps := range qpsByUpstream {
		upstream := &mockRateLimitedUpstream{
			address: net.IPv4(203, 0, 113, byte(upstreamIndex+1)).String(),
		}
		var resolver ILookup = upstream
		resolver = NewRateLimitResolver(resolver, qps)
		resolver = NewCircuitBreakerResolver(resolver, 100)
		upstreams = append(upstreams, upstream)
		resolvers = append(resolvers, resolver)
		nominalQPS += qps
	}

	base := NewLoadBalanceResolver(NewRandomBalancer(), resolvers...)
	primary := NewCustomResolverFromBase(
		base,
		WithFollowCname(),
		WithCacheResolver(NewLRU(totalRequests*2, time.Minute)),
		WithRetry(3),
	)
	fallbackCalls := atomic.Uint64{}
	secondary := lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		fallbackCalls.Add(1)
		return newTestResponse(name, qtype), nil
	})
	resolver := NewFallbackResolver(primary, secondary)

	start := make(chan struct{})
	errCh := make(chan error, totalRequests)
	nextRequest := atomic.Uint64{}
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrency)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	for workerIndex := 0; workerIndex < concurrency; workerIndex++ {
		go func() {
			defer waitGroup.Done()
			<-start
			for {
				requestIndex := int(nextRequest.Add(1) - 1)
				if requestIndex >= totalRequests {
					return
				}
				name := fmt.Sprintf("load-%d.example", requestIndex)
				response, err := resolver.Lookup(ctx, name, dns.TypeA)
				if err != nil {
					errCh <- err
					continue
				}
				if response == nil || len(response.Answer) != 1 {
					errCh <- errors.New("resolver returned an invalid response")
				}
			}
		}()
	}
	startedAt := time.Now()
	close(start)
	waitGroup.Wait()
	elapsed := time.Since(startedAt)
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	if calls := fallbackCalls.Load(); calls != 0 {
		t.Fatalf("secondary resolver was called %d times, want 0", calls)
	}

	totalCalls := 0
	minimumCalls := totalRequests
	maximumCalls := 0
	for upstreamIndex, upstream := range upstreams {
		requestTimes := upstream.requestTimes()
		calls := len(requestTimes)
		totalCalls += calls
		minimumCalls = min(minimumCalls, calls)
		maximumCalls = max(maximumCalls, calls)
		if calls < 2 {
			continue
		}
		minimumSpan := time.Duration(calls-1) * time.Second / time.Duration(qpsByUpstream[upstreamIndex])
		actualSpan := requestTimes[calls-1].Sub(requestTimes[0])
		if actualSpan < minimumSpan*85/100 {
			t.Fatalf(
				"upstream %d exceeded %d qps: %d requests arrived in %s, minimum safe span %s",
				upstreamIndex,
				qpsByUpstream[upstreamIndex],
				calls,
				actualSpan,
				minimumSpan,
			)
		}
	}
	if totalCalls != totalRequests {
		t.Fatalf("upstreams received %d requests, want %d", totalCalls, totalRequests)
	}

	actualQPS := float64(totalRequests) / elapsed.Seconds()
	t.Logf(
		"legacy random pool served %d requests with concurrency %d in %s: %.1f qps of %d nominal qps; per-upstream calls %d..%d",
		totalRequests,
		concurrency,
		elapsed,
		actualQPS,
		nominalQPS,
		minimumCalls,
		maximumCalls,
	)
	return legacyRandomPoolLoadResult{actualQPS: actualQPS, nominalQPS: nominalQPS}
}

func TestFallbackResolver_RateLimitedPoolSuccessDoesNotFallback(t *testing.T) {
	_, resolvers := newRateLimitedUpstreams(nil)
	primary := NewLoadBalanceResolver(NewRoundRobinBalancer(), resolvers...)
	fallbackCalls := atomic.Uint64{}
	secondary := lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		fallbackCalls.Add(1)
		return newTestResponse(name, qtype), nil
	})
	resolver := NewFallbackResolver(primary, secondary)

	for requestIndex := 0; requestIndex < mockUpstreamCount; requestIndex++ {
		response, err := resolver.Lookup(context.Background(), "success.example", dns.TypeA)
		if err != nil || response == nil {
			t.Fatalf("request %d returned response=%v error=%v", requestIndex, response, err)
		}
	}
	if calls := fallbackCalls.Load(); calls != 0 {
		t.Fatalf("secondary resolver was called %d times, want 0", calls)
	}
}

func TestFallbackResolver_AllRateLimitedUpstreamsFailReturnsSecondaryResponse(t *testing.T) {
	primaryErr := errors.New("upstream unavailable")
	upstreams, resolvers := newRateLimitedUpstreams(primaryErr)
	primary := NewRetryResolver(
		len(resolvers),
		NewLoadBalanceResolver(NewRoundRobinBalancer(), resolvers...),
	)
	secondaryResponse := newTestResponse("fallback.example", dns.TypeA)
	secondaryCalls := atomic.Uint64{}
	secondary := lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		secondaryCalls.Add(1)
		return secondaryResponse, nil
	})
	resolver := NewFallbackResolver(primary, secondary)

	response, err := resolver.Lookup(context.Background(), "fallback.example", dns.TypeA)
	if err != nil || response != secondaryResponse {
		t.Fatalf("got response=%p error=%v, want fallback response=%p and nil error", response, err, secondaryResponse)
	}
	if calls := secondaryCalls.Load(); calls != 1 {
		t.Fatalf("secondary resolver was called %d times, want 1", calls)
	}
	for upstreamIndex, upstream := range upstreams {
		if calls := upstream.calls.Load(); calls != 1 {
			t.Fatalf("upstream %d was tried %d times, want 1", upstreamIndex, calls)
		}
	}
}

func newRateLimitedUpstreams(upstreamErr error) (
	upstreams []*mockRateLimitedUpstream,
	resolvers []ILookup,
) {
	upstreams = make([]*mockRateLimitedUpstream, 0, mockUpstreamCount)
	resolvers = make([]ILookup, 0, mockUpstreamCount)
	for upstreamIndex := 0; upstreamIndex < mockUpstreamCount; upstreamIndex++ {
		upstream := &mockRateLimitedUpstream{
			address: net.IPv4(192, 0, 2, byte(upstreamIndex+1)).String(),
			err:     upstreamErr,
		}
		upstreams = append(upstreams, upstream)
		resolvers = append(resolvers, NewRateLimitResolver(upstream, mockUpstreamQPS))
	}
	return upstreams, resolvers
}
