package fastresolver

import (
	"context"
	"slices"
	"strings"
	"time"

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
		resolver, err := NewResolver(ns)
		if err != nil {
			continue
		}
		resolvers = append(resolvers, resolver)
	}
	if len(resolvers) == 0 {
		resolvers = slices.Clone(rootResolvers)
	}
	for i := 0; i < 16; i++ {
		resolver := NewRetryResolver(len(resolvers), NewLoadBalanceResolver(NewRandomBalancer(), resolvers...))
		resp, err := resolver.Lookup(ctx, qname, qtype)
		if err != nil {
			return DNSRR{}, err
		}
		if resp.Authoritative || resp.NXDomain {
			cacheForRecursive.Set(qname, qtype, resp)
			return resp, nil
		}
		if len(resp.AuthNS) > 0 {
			resolvers = resolvers[:0]
			for _, ns := range resp.AuthNS {
				resolver, err := NewResolver(ns)
				if err != nil {
					continue
				}
				resolvers = append(resolvers, resolver)
			}
			continue
		}
		break
	}
	return DNSRR{NXDomain: true}, err
}

func tldPlusOne(name string) string {
	domain, err := publicsuffix.EffectiveTLDPlusOne(strings.TrimSuffix(name, "."))
	if err != nil {
		return name
	}
	return domain
}
