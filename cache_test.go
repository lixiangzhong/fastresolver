package fastresolver

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

type mockSlowResolver struct {
	calls int64
}

func (m *mockSlowResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	atomic.AddInt64(&m.calls, 1)
	// Simulate query delay to allow concurrent lookups to overlap and merge
	time.Sleep(20 * time.Millisecond)
	return DNSRR{
		A: []string{"1.2.3.4"},
	}, nil
}

func TestCacheResolver_Singleflight(t *testing.T) {
	mock := &mockSlowResolver{}
	// Create a temporary test cache
	cache := NewLRU(10, 5*time.Second)
	resolver := NewCacheResolver(cache, mock)

	var wg sync.WaitGroup
	concurrentCount := 100
	wg.Add(concurrentCount)

	// Launch 100 concurrent queries for the same domain
	for i := 0; i < concurrentCount; i++ {
		go func() {
			defer wg.Done()
			_, err := resolver.Lookup(context.Background(), "test.example", dns.TypeA)
			if err != nil {
				t.Errorf("Lookup failed: %v", err)
			}
		}()
	}

	wg.Wait()

	// Verify that the underlying resolver was called exactly once
	gotCalls := atomic.LoadInt64(&mock.calls)
	if gotCalls != 1 {
		t.Fatalf("expected exactly 1 call to the underlying resolver, got %d", gotCalls)
	}

	// Make a subsequent call that must hit the cache immediately
	_, err := resolver.Lookup(context.Background(), "test.example", dns.TypeA)
	if err != nil {
		t.Fatalf("subsequent lookup failed: %v", err)
	}

	gotCalls = atomic.LoadInt64(&mock.calls)
	if gotCalls != 1 {
		t.Fatalf("expected call count to remain 1 after cache hit, got %d", gotCalls)
	}
}

type mockTTLResolver struct {
	calls int64
	ttl   time.Duration
}

func (m *mockTTLResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	atomic.AddInt64(&m.calls, 1)
	return DNSRR{
		A:   []string{"1.2.3.4"},
		TTL: m.ttl,
	}, nil
}

func TestCacheResolver_DynamicTTL(t *testing.T) {
	mock := &mockTTLResolver{ttl: 1 * time.Second} // Raw TTL of 1 second
	cache := NewLRU(10, 5*time.Second)
	resolver := NewCacheResolver(cache, mock)

	ctx := context.Background()

	// First query: populates the cache
	_, err := resolver.Lookup(ctx, "dynamic.test", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for 1.1 seconds. If 1s TTL was respected, cache would expire.
	// But because of the "minimum 1-minute TTL" enforcement, it should hit the cache.
	time.Sleep(1100 * time.Millisecond)

	_, err = resolver.Lookup(ctx, "dynamic.test", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}

	gotCalls := atomic.LoadInt64(&mock.calls)
	if gotCalls != 1 {
		t.Fatalf("expected exactly 1 call due to minimum 1-minute TTL enforcement, got %d", gotCalls)
	}
}
