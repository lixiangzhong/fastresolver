package fastresolver

import (
	"context"

	"github.com/miekg/dns"
)

var _ Resolver = (*FallbackTrace)(nil)

type FallbackTrace struct {
	Resolver
}

// LookupIP implements Resolver.
func (f *FallbackTrace) LookupIP(ctx context.Context, name string) ([]string, error) {
	ret, err := f.Resolver.LookupIP(ctx, name)
	if err == nil {
		return ret, err
	}
	rr, err := trace(ctx, name, dns.TypeA)
	if err != nil {
		return ret, err
	}
	if len(rr.A) > 0 {
		return rr.A, nil
	}
	if len(rr.CNAME) > 0 {
		return f.Resolver.LookupIP(ctx, rr.CNAME[0])
	}
	return nil, nil
}

// LookupNS implements Resolver.
func (f *FallbackTrace) LookupNS(ctx context.Context, name string) ([]string, error) {
	ret, err := f.Resolver.LookupNS(ctx, name)
	if err == nil {
		return ret, err
	}
	rr, err := trace(ctx, name, dns.TypeNS)
	if err != nil {
		return ret, err
	}
	if len(rr.NS) > 0 {
		return rr.A, nil
	}
	return nil, nil
}
