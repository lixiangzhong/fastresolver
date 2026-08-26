package fastresolver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

type hiddenQoSUpstream struct {
	capacityQPS float64
	latency     time.Duration
	address     net.IP

	mu       sync.Mutex
	tokens   float64
	lastFill time.Time
	accepted []time.Time
	rejected []time.Time
}

func newHiddenQoSUpstream(capacityQPS float64, address net.IP) *hiddenQoSUpstream {
	now := time.Now()
	return &hiddenQoSUpstream{
		capacityQPS: capacityQPS,
		latency:     2 * time.Millisecond,
		address:     address,
		tokens:      1,
		lastFill:    now,
	}
}

func (upstream *hiddenQoSUpstream) Lookup(
	ctx context.Context,
	name string,
	qtype uint16,
) (*dns.Msg, error) {
	now := time.Now()
	upstream.mu.Lock()
	elapsed := now.Sub(upstream.lastFill).Seconds()
	upstream.tokens = min(1, upstream.tokens+elapsed*upstream.capacityQPS)
	upstream.lastFill = now
	if upstream.tokens < 1 {
		upstream.rejected = append(upstream.rejected, now)
		upstream.mu.Unlock()
		response := newTestResponse(name, qtype)
		response.Rcode = dns.RcodeServerFailure
		return response, nil
	}
	upstream.tokens--
	upstream.accepted = append(upstream.accepted, now)
	upstream.mu.Unlock()

	timer := time.NewTimer(upstream.latency)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}

	response := newTestResponse(name, qtype)
	response.Answer = []dns.RR{&dns.A{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(name),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    60,
		},
		A: upstream.address,
	}}
	return response, nil
}

func (upstream *hiddenQoSUpstream) countsSince(start time.Time) (accepted, rejected int) {
	upstream.mu.Lock()
	defer upstream.mu.Unlock()

	for _, timestamp := range upstream.accepted {
		if !timestamp.Before(start) {
			accepted++
		}
	}
	for _, timestamp := range upstream.rejected {
		if !timestamp.Before(start) {
			rejected++
		}
	}
	return accepted, rejected
}

func TestAdaptivePoolResolver_DiscoversHiddenQoSCapacity(t *testing.T) {
	capacities := []int{50, 60, 70, 80, 90, 100, 110, 120, 130, 140}
	upstreams := make([]*hiddenQoSUpstream, 0, len(capacities))
	resolvers := make([]ILookup, 0, len(capacities))
	nominalQPS := 0
	for upstreamIndex, capacity := range capacities {
		upstream := newHiddenQoSUpstream(
			float64(capacity),
			net.IPv4(192, 0, 2, byte(upstreamIndex+1)),
		)
		upstreams = append(upstreams, upstream)
		resolvers = append(resolvers, upstream)
		nominalQPS += capacity
	}

	pool, err := NewAdaptivePoolResolver(
		resolvers,
		WithAdaptiveInitialQPS(10),
		WithAdaptiveMinQPS(1),
		WithAdaptiveMaxQPS(150),
		WithAdaptiveIncreasePerSecond(50),
		WithAdaptiveDecreaseFactor(0.7),
		WithAdaptiveFailureCooldown(20*time.Millisecond),
		WithAdaptiveMaxAttempts(3),
	)
	if err != nil {
		t.Fatal(err)
	}

	const (
		concurrency    = 200
		testDuration   = 6 * time.Second
		warmupDuration = 2 * time.Second
	)
	startedAt := time.Now()
	measurementStart := startedAt.Add(warmupDuration)
	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()
	var requestID atomic.Uint64
	var applicationErrors atomic.Uint64
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrency)
	for workerIndex := 0; workerIndex < concurrency; workerIndex++ {
		go func() {
			defer waitGroup.Done()
			for ctx.Err() == nil {
				id := requestID.Add(1)
				_, lookupErr := pool.Lookup(ctx, fmt.Sprintf("adaptive-%d.example", id), dns.TypeA)
				if lookupErr != nil && ctx.Err() == nil {
					applicationErrors.Add(1)
				}
			}
		}()
	}
	waitGroup.Wait()

	measurementDuration := time.Since(measurementStart)
	totalAccepted := 0
	totalRejected := 0
	for upstreamIndex, upstream := range upstreams {
		accepted, rejected := upstream.countsSince(measurementStart)
		if accepted == 0 {
			t.Fatalf("upstream %d received no successful requests", upstreamIndex)
		}
		totalAccepted += accepted
		totalRejected += rejected
	}
	actualQPS := float64(totalAccepted) / measurementDuration.Seconds()
	rejectionRate := float64(totalRejected) / float64(totalAccepted+totalRejected)
	stats := pool.Stats()
	for upstreamIndex, stat := range stats {
		if stat.InFlight != 0 {
			t.Fatalf("upstream %d retained %d in-flight requests", upstreamIndex, stat.InFlight)
		}
	}
	t.Logf(
		"adaptive pool served %d successful requests in %s: %.1f qps of %d hidden qps; qos rejects=%d (%.2f%%); application errors=%d; estimates=%v",
		totalAccepted,
		measurementDuration,
		actualQPS,
		nominalQPS,
		totalRejected,
		rejectionRate*100,
		applicationErrors.Load(),
		estimatedQPS(stats),
	)
	if actualQPS < float64(nominalQPS)*0.65 {
		t.Fatalf("adaptive throughput %.1f qps is below 65%% of hidden capacity %d", actualQPS, nominalQPS)
	}
	if rejectionRate > 0.1 {
		t.Fatalf("qos rejection rate %.2f%% exceeds 10%%", rejectionRate*100)
	}
	if errorsCount := applicationErrors.Load(); errorsCount > uint64(totalAccepted/100) {
		t.Fatalf("application errors %d exceed 1%% of successful requests %d", errorsCount, totalAccepted)
	}
}

func TestAdaptivePoolResolver_AllFailuresFallbackWithoutError(t *testing.T) {
	failedResolvers := make([]ILookup, 3)
	for resolverIndex := range failedResolvers {
		failedResolvers[resolverIndex] = lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
			response := newTestResponse(name, qtype)
			response.Rcode = dns.RcodeServerFailure
			return response, nil
		})
	}
	pool, err := NewAdaptivePoolResolver(failedResolvers, WithAdaptiveMaxAttempts(3))
	if err != nil {
		t.Fatal(err)
	}
	secondaryResponse := newTestResponse("fallback.example", dns.TypeA)
	secondaryCalls := atomic.Uint64{}
	secondary := lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		secondaryCalls.Add(1)
		return secondaryResponse, nil
	})
	resolver := NewFallbackResolver(pool, secondary)

	response, err := resolver.Lookup(context.Background(), "fallback.example", dns.TypeA)
	if err != nil || response != secondaryResponse {
		t.Fatalf("got response=%p error=%v, want response=%p and nil error", response, err, secondaryResponse)
	}
	if calls := secondaryCalls.Load(); calls != 1 {
		t.Fatalf("secondary resolver was called %d times, want 1", calls)
	}
}

func TestAdaptivePoolResolver_DeadlineExceededDecreasesRate(t *testing.T) {
	started := make(chan struct{})
	upstream := lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	pool, err := NewAdaptivePoolResolver(
		[]ILookup{upstream},
		WithAdaptiveInitialQPS(20),
		WithAdaptiveDecreaseFactor(0.5),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err = pool.Lookup(ctx, "timeout.example", dns.TypeA)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got error %v, want context deadline exceeded", err)
	}
	<-started
	stat := pool.Stats()[0]
	if stat.EstimatedQPS != 10 || stat.Failures != 1 || stat.InFlight != 0 {
		t.Fatalf("deadline did not decrease upstream capacity: %+v", stat)
	}
}

func TestAdaptivePoolResolver_CanceledContextDoesNotDecreaseRate(t *testing.T) {
	started := make(chan struct{})
	upstream := lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	pool, err := NewAdaptivePoolResolver(
		[]ILookup{upstream},
		WithAdaptiveInitialQPS(20),
		WithAdaptiveDecreaseFactor(0.5),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	_, err = pool.Lookup(ctx, "cancel.example", dns.TypeA)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got error %v, want context canceled", err)
	}
	stat := pool.Stats()[0]
	if stat.EstimatedQPS != 20 || stat.Failures != 0 || stat.InFlight != 0 {
		t.Fatalf("caller cancellation changed upstream capacity: %+v", stat)
	}
}

func TestNewAdaptivePoolResolver_ValidatesConfiguration(t *testing.T) {
	resolver := lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		return newTestResponse(name, qtype), nil
	})
	tests := []struct {
		name      string
		options   []AdaptivePoolOption
		resolvers []ILookup
	}{
		{name: "no resolvers"},
		{name: "nan initial qps", options: []AdaptivePoolOption{WithAdaptiveInitialQPS(math.NaN())}, resolvers: []ILookup{resolver}},
		{name: "invalid decrease", options: []AdaptivePoolOption{WithAdaptiveDecreaseFactor(1)}, resolvers: []ILookup{resolver}},
		{name: "nil option", options: []AdaptivePoolOption{nil}, resolvers: []ILookup{resolver}},
		{name: "nil resolver", resolvers: []ILookup{nil}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewAdaptivePoolResolver(test.resolvers, test.options...)
			if err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestNewAdaptivePoolResolver_DefaultsAndOverrides(t *testing.T) {
	resolver := lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		return newTestResponse(name, qtype), nil
	})
	pool, err := NewAdaptivePoolResolver([]ILookup{resolver})
	if err != nil {
		t.Fatal(err)
	}
	if pool.options != (AdaptivePoolOptions{
		InitialQPS:        10,
		MinQPS:            1,
		MaxQPS:            1_000,
		IncreasePerSecond: 5,
		DecreaseFactor:    0.7,
		FailureCooldown:   100 * time.Millisecond,
		MaxAttempts:       1,
	}) {
		t.Fatalf("unexpected defaults: %+v", pool.options)
	}

	pool, err = NewAdaptivePoolResolver(
		[]ILookup{resolver, resolver},
		WithAdaptiveInitialQPS(20),
		WithAdaptiveInitialQPS(25),
		WithAdaptiveFailureCooldown(0),
		WithAdaptiveMaxAttempts(2),
	)
	if err != nil {
		t.Fatal(err)
	}
	if pool.options.InitialQPS != 25 || pool.options.FailureCooldown != 0 || pool.options.MaxAttempts != 2 {
		t.Fatalf("options were not applied: %+v", pool.options)
	}
}

func estimatedQPS(stats []AdaptiveUpstreamStats) []int {
	estimates := make([]int, len(stats))
	for statsIndex, stat := range stats {
		estimates[statsIndex] = int(stat.EstimatedQPS + 0.5)
	}
	return estimates
}
