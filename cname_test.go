package fastresolver

import (
	"context"
	"errors"
	"testing"

	"github.com/miekg/dns"
)

type loopCnameResolver struct{}

func (loopCnameResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	return DNSRR{
		CNAME: []string{name},
	}, nil
}

func TestFollowCnameResolver_LookupStopsOnCnameLoop(t *testing.T) {
	resolver := NewFollowCnameResolver(loopCnameResolver{})

	_, err := resolver.Lookup(context.Background(), "loop.example", dns.TypeA)
	if !errors.Is(err, ErrCnameDepthExceeded) {
		t.Fatalf("got error %v, want %v", err, ErrCnameDepthExceeded)
	}
}
