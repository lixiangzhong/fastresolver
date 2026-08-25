package fastresolver

import (
	"context"
	"errors"
	"testing"

	"github.com/miekg/dns"
)

func TestConcurrencyResolver_Lookup(t *testing.T) {
	winner := newTestResponse("example.com", dns.TypeA)
	resolver := NewConcurrencyResolver(
		lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
			return nil, errors.New("failed")
		}),
		lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
			return winner, nil
		}),
	)

	response, err := resolver.Lookup(context.Background(), "example.com", dns.TypeA)
	if err != nil || response != winner {
		t.Fatalf("got response=%p error=%v, want winner=%p", response, err, winner)
	}
}

func TestConcurrencyResolver_WithoutResolvers(t *testing.T) {
	response, err := NewConcurrencyResolver().Lookup(context.Background(), "example.com", dns.TypeA)
	if response != nil || !errors.Is(err, ErrNoResolver) {
		t.Fatalf("got response=%v error=%v, want ErrNoResolver", response, err)
	}
}

func TestConcurrencyResolver_AllFailuresPreserveResponse(t *testing.T) {
	failedResponse := newTestResponse("example.com", dns.TypeA)
	firstError := errors.New("first")
	secondError := errors.New("second")
	resolver := NewConcurrencyResolver(
		lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
			return failedResponse, firstError
		}),
		lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
			return nil, secondError
		}),
	)

	response, err := resolver.Lookup(context.Background(), "example.com", dns.TypeA)
	if response != failedResponse || !errors.Is(err, firstError) || !errors.Is(err, secondError) {
		t.Fatalf("got response=%p error=%v", response, err)
	}
}

func TestConcurrencyResolver_NormalizesNoResponse(t *testing.T) {
	resolver := NewConcurrencyResolver(lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		return nil, nil
	}))

	response, err := resolver.Lookup(context.Background(), "example.com", dns.TypeA)
	if response != nil || !errors.Is(err, ErrNoResponse) {
		t.Fatalf("got response=%v error=%v, want ErrNoResponse", response, err)
	}
}

func TestConcurrencyResolver_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolver := NewConcurrencyResolver(lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))

	response, err := resolver.Lookup(ctx, "example.com", dns.TypeA)
	if response != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("got response=%v error=%v, want context canceled", response, err)
	}
}
