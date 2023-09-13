package fastresolver

import (
	"context"
	"slices"
	"strings"

	"github.com/miekg/dns"
	"github.com/zonedb/zonedb"
	"golang.org/x/net/publicsuffix"
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

func trace(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	var nxdomain DNSRR = DNSRR{NXDomain: true}
	var upstreams Upstreams
	z := zonedb.PublicZone(tldPlusOne(name))
	if z == nil {
		return nxdomain, nil
	}
	for _, ns := range z.NameServers {
		upstreams = append(upstreams, Upstream{
			Addr: ns,
		})
	}
	if len(upstreams) == 0 {
		upstreams = slices.Clone(roots)
	}
	var err error
	for i := 0; i < 16; i++ {
		resolver := NewRetryResolver(3, NewFailoverResovler(100, upstreams...))
		var rsp DNSRR
		var hitcache bool
		rsp, hitcache = DefalutCache.Get(name, qtype)
		if !hitcache {
			rsp, err = resolver.Lookup(ctx, name, qtype)
			if err != nil {
				continue
			}
		}
		if rsp.Authoritative || rsp.NXDomain {
			DefalutCache.Set(name, qtype, rsp)
			return rsp, nil
		}
		if len(rsp.AuthNS) > 0 {
			upstreams = upstreams[:0]
			for _, ns := range rsp.AuthNS {
				upstreams = append(upstreams, Upstream{
					Addr: ns,
				})
			}
			continue
		}
		break
	}
	return nxdomain, err
}

func tldPlusOne(name string) string {
	domain, err := publicsuffix.EffectiveTLDPlusOne(strings.TrimSuffix(name, "."))
	if err != nil {
		return name
	}
	return domain
}
