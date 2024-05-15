package fastresolver

import (
	"context"
	"errors"
	"testing"
)

func TestRoundRobinBalancer_Choose(t *testing.T) {
	s1 := successResolver("s1")
	s2 := successResolver("s2")
	f1 := failureResolver("f1")
	f2 := failureResolver("f2")
	ss := []ILookup{s1, s2, f1, f2}
	rr := NewRoundRobinBalancer()
	for i := 0; i < 10; i++ {
		got := rr.Choose(ss)
		want := ss[i%len(ss)]
		if got != want {
			t.Fatal(i, got, want)
		}
	}
}

type successResolver string

func (s successResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	return DNSRR{}, nil
}

type failureResolver string

func (f failureResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	return DNSRR{}, errors.New("failureResolver")
}

func TestCircuitBreakerRoundRobinBalancer_Choose(t *testing.T) {
	s1 := successResolver("s1")
	s2 := successResolver("s2")
	f1 := failureResolver("f1")
	f2 := failureResolver("f2")
	ss := []ILookup{
		NewCircuitBreakerResolver(s1, 1),
		NewCircuitBreakerResolver(s2, 1),
		NewCircuitBreakerResolver(f1, 1),
		NewCircuitBreakerResolver(f2, 1),
	}
	ss[0].Lookup(context.Background(), "", 0)
	ss[1].Lookup(context.Background(), "", 0)
	ss[2].Lookup(context.Background(), "", 0)
	ss[3].Lookup(context.Background(), "", 0)
	rr := NewRoundRobinBalancer()
	for i := 0; i < 10; i++ {
		got := rr.Choose(ss).(*CircuitBreakerResolver).resolver.resolver
		t.Log(got)
	}
}

func TestRandomBalancer_Choose(t *testing.T) {
	s1 := successResolver("s1")
	s2 := successResolver("s2")
	f1 := failureResolver("f1")
	f2 := failureResolver("f2")
	ss := []ILookup{s1, s2, f1, f2}
	rb := NewRandomBalancer()

	for i := 0; i < 10; i++ {
		got := rb.Choose(ss)
		t.Log(got)
	}
}

func TestCircuitBreakerRandomBalancer_Choose(t *testing.T) {
	s1 := successResolver("s1")
	s2 := successResolver("s2")
	f1 := failureResolver("f1")
	f2 := failureResolver("f2")
	ss := []ILookup{
		NewCircuitBreakerResolver(s1, 1),
		NewCircuitBreakerResolver(s2, 1),
		NewCircuitBreakerResolver(f1, 1),
		NewCircuitBreakerResolver(f2, 1),
	}
	ss[0].Lookup(context.Background(), "", 0)
	ss[1].Lookup(context.Background(), "", 0)
	ss[2].Lookup(context.Background(), "", 0)
	ss[3].Lookup(context.Background(), "", 0)
	rb := NewRandomBalancer()
	for i := 0; i < 10; i++ {
		got := rb.Choose(ss).(*CircuitBreakerResolver).resolver.resolver
		t.Log(got)
	}
}
