package fastresolver

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestRateLimitResolver_ContextDeadlineStopsWaiting(t *testing.T) {
	calls := atomic.Uint64{}
	underlying := lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		calls.Add(1)
		return newTestResponse(name, qtype), nil
	})
	resolver := NewRateLimitResolver(underlying, 1)
	if _, err := resolver.Lookup(context.Background(), "first.example", dns.TypeA); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	_, err := resolver.Lookup(ctx, "second.example", dns.TypeA)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got error %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 200*time.Millisecond {
		t.Fatalf("rate limiter ignored context deadline for %s", elapsed)
	}
	if gotCalls := calls.Load(); gotCalls != 1 {
		t.Fatalf("underlying resolver was called %d times, want 1", gotCalls)
	}
}
