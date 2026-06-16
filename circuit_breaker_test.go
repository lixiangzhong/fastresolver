package fastresolver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/miekg/dns"
)

type mockFailingResolver struct {
	shouldFail bool
}

func (m *mockFailingResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	if m.shouldFail {
		return DNSRR{}, errors.New("resolver query failed")
	}
	return DNSRR{A: []string{"1.1.1.1"}}, nil
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	mock := &mockFailingResolver{}
	// Threshold: 2 failures; Cooling timeout: 50 milliseconds
	cooling := 50 * time.Millisecond
	cb := NewCircuitBreakerResolverWithCooling(mock, 2, cooling)

	ctx := context.Background()

	// 1. Initial StateClosed should succeed
	mock.shouldFail = false
	_, err := cb.Lookup(ctx, "test", dns.TypeA)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cb.getState() != StateClosed {
		t.Fatalf("expected state Closed, got %v", cb.getState())
	}

	// 2. Trigger StateOpen by hitting the failure threshold
	mock.shouldFail = true
	// Failure 1
	_, _ = cb.Lookup(ctx, "test", dns.TypeA)
	if cb.getState() != StateClosed {
		t.Fatalf("expected state Closed after 1 failure, got %v", cb.getState())
	}
	// Failure 2 -> triggers Open state
	_, _ = cb.Lookup(ctx, "test", dns.TypeA)
	if cb.getState() != StateOpen {
		t.Fatalf("expected state Open after 2 failures, got %v", cb.getState())
	}

	// 3. Under StateOpen, queries should short-circuit and fail immediately
	_, err = cb.Lookup(ctx, "test", dns.TypeA)
	if !errors.Is(err, ErrCircuitBreaker) {
		t.Fatalf("expected ErrCircuitBreaker, got %v", err)
	}

	// 4. Wait for cooling timeout, Accept should allow probing (which leads to HalfOpen)
	time.Sleep(cooling + 10*time.Millisecond)
	if !cb.Accept() {
		t.Fatal("expected Accept to be true after cooling time elapsed")
	}

	// 5. Under StateHalfOpen, a failed probe must kick it back to StateOpen and reset cooling timer
	mock.shouldFail = true
	_, err = cb.Lookup(ctx, "test", dns.TypeA)
	if err == nil || errors.Is(err, ErrCircuitBreaker) {
		t.Fatalf("expected raw resolver error in HalfOpen, got %v", err)
	}
	if cb.getState() != StateOpen {
		t.Fatalf("expected state to return to Open, got %v", cb.getState())
	}

	// Subsequent query should short-circuit again
	_, err = cb.Lookup(ctx, "test", dns.TypeA)
	if !errors.Is(err, ErrCircuitBreaker) {
		t.Fatalf("expected ErrCircuitBreaker, got %v", err)
	}

	// 6. Wait again, probe succeeds, which should self-heal and return to StateClosed
	time.Sleep(cooling + 10*time.Millisecond)
	mock.shouldFail = false

	_, err = cb.Lookup(ctx, "test", dns.TypeA)
	if err != nil {
		t.Fatalf("expected success in HalfOpen, got %v", err)
	}
	if cb.getState() != StateClosed {
		t.Fatalf("expected state to recover to Closed, got %v", cb.getState())
	}

	// Under Closed state, check if failure count was reset (so 1 failure shouldn't immediately trigger Open)
	mock.shouldFail = true
	_, _ = cb.Lookup(ctx, "test", dns.TypeA)
	if cb.getState() != StateClosed {
		t.Fatalf("expected state to remain Closed after 1 post-recovery failure, got %v", cb.getState())
	}
}

func TestCircuitBreaker_ConsecutiveFailures(t *testing.T) {
	mock := &mockFailingResolver{}
	// 失败阈值：3
	cb := NewCircuitBreakerResolver(mock, 3)
	ctx := context.Background()

	// 1. 制造 2 次失败
	mock.shouldFail = true
	_, _ = cb.Lookup(ctx, "test", dns.TypeA)
	_, _ = cb.Lookup(ctx, "test", dns.TypeA)
	if cb.getState() != StateClosed {
		t.Fatalf("expected state to be Closed after 2 failures, got %v", cb.getState())
	}

	// 2. 插入一次成功，应当重置失败计数器
	mock.shouldFail = false
	_, err := cb.Lookup(ctx, "test", dns.TypeA)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	// 3. 再发生第 3 次（实际由于被清零而当作第 1 次）失败，状态机应该依然是 Closed
	mock.shouldFail = true
	_, _ = cb.Lookup(ctx, "test", dns.TypeA)
	if cb.getState() != StateClosed {
		t.Fatalf("expected state to remain Closed since failure count was reset, got %v", cb.getState())
	}

	// 4. 连续追加 2 次失败（累计连续 3 次），熔断器应该进入 Open 状态
	_, _ = cb.Lookup(ctx, "test", dns.TypeA)
	_, _ = cb.Lookup(ctx, "test", dns.TypeA)
	if cb.getState() != StateOpen {
		t.Fatalf("expected state to trigger Open after 3 consecutive failures, got %v", cb.getState())
	}
}
