package fastresolver

import (
	"context"

	"github.com/miekg/dns"
)

var _ Resolver = (*FallbackTrace)(nil)

type FallbackTrace struct {
	Resolver
}

func (f *FallbackTrace) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	ret, err := f.Resolver.Lookup(ctx, name, qtype)
	if err == nil {
		return ret, err
	}
	return trace(ctx, name, qtype)
}

// LookupIP implements Resolver.
func (f *FallbackTrace) LookupIP(ctx context.Context, name string) ([]string, error) {
	var ret []string
	rr, err := f.Lookup(ctx, name, dns.TypeA)
	if err != nil {
		return ret, err
	}
	if len(rr.A) > 0 {
		return rr.A, nil
	}
	if len(rr.CNAME) > 0 {
		return f.Resolver.LookupIP(ctx, rr.CNAME[0])
	}
	return ret, nil
}

// LookupNS implements Resolver.
func (f *FallbackTrace) LookupNS(ctx context.Context, name string) ([]string, error) {
	var ret []string
	rr, err := f.Lookup(ctx, name, dns.TypeNS)
	if err != nil {
		return ret, err
	}
	if len(rr.NS) > 0 {
		return rr.A, nil
	}
	return ret, nil
}
