package fastresolver_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	fastresolver "github.com/lixiangzhong/fastresolver/v3"
	"github.com/miekg/dns"
)

type countingResolver struct {
	calls atomic.Uint64
}

func (resolver *countingResolver) Lookup(
	_ context.Context,
	name string,
	qtype uint16,
) (*dns.Msg, error) {
	resolver.calls.Add(1)
	request := new(dns.Msg).SetQuestion(dns.Fqdn(name), qtype)
	return new(dns.Msg).SetReply(request), nil
}

func TestV3_RateLimitWaitHonorsContextDeadline(t *testing.T) {
	underlying := &countingResolver{}
	resolver := fastresolver.NewRateLimitResolver(underlying, 1)
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
	if calls := underlying.calls.Load(); calls != 1 {
		t.Fatalf("underlying resolver received %d calls, want 1", calls)
	}
}
