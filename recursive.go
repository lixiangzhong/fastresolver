package fastresolver

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/zonedb/zonedb"
	"golang.org/x/net/publicsuffix"
)

var cacheForRecursive = NewLRU(5000, time.Minute)
var rootResolvers []ILookup

func init() {
	for _, ns := range []string{
		"a.root-servers.net",
		"b.root-servers.net",
		"c.root-servers.net",
		"d.root-servers.net",
		"e.root-servers.net",
		"f.root-servers.net",
		"g.root-servers.net",
		"h.root-servers.net",
		"i.root-servers.net",
		"j.root-servers.net",
		"k.root-servers.net",
		"l.root-servers.net",
		"m.root-servers.net",
	} {
		resolver, err := NewResolver(ns)
		if err != nil {
			continue
		}
		rootResolvers = append(rootResolvers, resolver)
	}
}

func RecursiveLookup(ctx context.Context, qname string, qtype uint16) (dnsrr DNSRR, err error) {
	resp, hit := cacheForRecursive.Get(qname, qtype)
	if hit {
		return resp, nil
	}
	z := zonedb.PublicZone(tldPlusOne(qname))
	if z == nil {
		dnsrr.NXDomain = true
		return
	}
	resolvers := make([]ILookup, 0)
	for _, ns := range z.NameServers {
		if rr, err := internalResolver.Lookup(ctx, ns, dns.TypeA); err == nil && len(rr.A) > 0 {
			for _, ip := range rr.A {
				resolver, err := NewResolver(ip)
				if err != nil {
					continue
				}
				resolvers = append(resolvers, resolver)
			}
		} else {
			resolver, err := NewResolver(ns)
			if err != nil {
				continue
			}
			resolvers = append(resolvers, resolver)
		}
	}
	if len(resolvers) == 0 {
		resolvers = slices.Clone(rootResolvers)
	}
	for i := 0; i < 16; i++ {
		resolver := NewRetryResolver(len(resolvers), NewLoadBalanceResolver(NewRandomBalancer(), resolvers...))
		resp, err := resolver.Lookup(ctx, qname, qtype)
		if err != nil {
			return resp, err
		}
		if resp.Authoritative || resp.NXDomain {
			cacheForRecursive.Set(qname, qtype, resp)
			return resp, nil
		}
		if len(resp.AuthNS) > 0 {
			qnameNS := make([]string, 0)
			resolvers = resolvers[:0]
			for _, ns := range resp.AuthNS {
				if qtype == dns.TypeNS && dns.Fqdn(ns.Name) == dns.Fqdn(qname) {
					qnameNS = append(qnameNS, ns.Value)
					continue
				}
				resolver, err := NewResolver(ns.Value)
				if err != nil {
					continue
				}
				resolvers = append(resolvers, resolver)
			}
			if len(qnameNS) > 0 {
				resp.NS = qnameNS
				return resp, nil
			}
			continue
		}
		return resp, nil
	}
	return DNSRR{}, err
}

func tldPlusOne(name string) string {
	domain, err := publicsuffix.EffectiveTLDPlusOne(strings.TrimSuffix(name, "."))
	if err != nil {
		return name
	}
	return domain
}

type RecursiveResolver struct{}

func (r *RecursiveResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	return RecursiveLookup(ctx, name, qtype)
}

type FallbackResolver struct {
	primary   ILookup
	secondary ILookup
}

func NewFallbackResolver(primary ILookup, secondary ILookup) ILookup {
	return &FallbackResolver{
		primary:   primary,
		secondary: secondary,
	}
}

func (r *FallbackResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	resp, err := r.primary.Lookup(ctx, name, qtype)
	if err != nil {
		resp, err = r.secondary.Lookup(ctx, name, qtype)
	}
	return resp, err
}
