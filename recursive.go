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

// defaultRecursiveMaxDepth preserves the existing recursion limit.
const defaultRecursiveMaxDepth = 16

// resolverFactory keeps recursive transport construction deterministic in tests.
type resolverFactory func(server string) (ILookup, error)

var cacheForRecursive = NewLRU(5000, time.Minute)
var rootResolvers []ILookup

func init() {
	for _, nameServer := range []string{
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
		resolver, err := NewResolver(nameServer)
		if err == nil {
			rootResolvers = append(rootResolvers, resolver)
		}
	}
}

func RecursiveLookup(ctx context.Context, qname string, qtype uint16) (*dns.Msg, error) {
	return recursiveLookup(ctx, qname, qtype, func(server string) (ILookup, error) {
		return NewResolver(server)
	})
}

// recursiveLookup performs the existing iterative delegation flow with an injectable factory.
func recursiveLookup(ctx context.Context, qname string, qtype uint16, newResolver resolverFactory) (*dns.Msg, error) {
	if response, hit := cacheForRecursive.Get(qname, qtype); hit {
		return response, nil
	}
	zone := zonedb.PublicZone(tldPlusOne(qname))
	if zone == nil {
		request := new(dns.Msg).SetQuestion(dns.Fqdn(qname), qtype)
		return new(dns.Msg).SetRcode(request, dns.RcodeNameError), nil
	}

	resolvers := make([]ILookup, 0, len(zone.NameServers))
	for _, nameServer := range zone.NameServers {
		response, err := normalizeLookupResult(internalResolver.Lookup(ctx, nameServer, dns.TypeA))
		addresses := answerIPv4Addresses(response)
		if err == nil && len(addresses) > 0 {
			for _, address := range addresses {
				resolver, resolverErr := newResolver(address)
				if resolverErr == nil {
					resolvers = append(resolvers, resolver)
				}
			}
			continue
		}
		resolver, resolverErr := newResolver(nameServer)
		if resolverErr == nil {
			resolvers = append(resolvers, resolver)
		}
	}
	if len(resolvers) == 0 {
		resolvers = slices.Clone(rootResolvers)
	}

	var lastResponse *dns.Msg
	for depth := 0; depth < defaultRecursiveMaxDepth; depth++ {
		if len(resolvers) == 0 {
			return lastResponse, ErrNoResolver
		}
		resolver := NewRetryResolver(len(resolvers), NewLoadBalanceResolver(NewRandomBalancer(), resolvers...))
		response, err := normalizeLookupResult(resolver.Lookup(ctx, qname, qtype))
		if err != nil {
			return response, err
		}
		lastResponse = response
		if response.Authoritative || isTerminalNegative(response, qname, qtype) {
			cacheForRecursive.Set(qname, qtype, response)
			return response, nil
		}

		nameServers := authorityNameServers(response)
		if len(nameServers) == 0 {
			return response, nil
		}
		resolvers = resolvers[:0]
		for _, nameServer := range nameServers {
			if qtype == dns.TypeNS && dns.Fqdn(nameServer.Hdr.Name) == dns.Fqdn(qname) {
				return response, nil
			}
			resolver, resolverErr := newResolver(nameServer.Ns)
			if resolverErr == nil {
				resolvers = append(resolvers, resolver)
			}
		}
	}
	return lastResponse, ErrMaxRecursionDepth
}

// answerIPv4Addresses returns only A records from the Answer section.
func answerIPv4Addresses(response *dns.Msg) []string {
	if response == nil {
		return nil
	}
	addresses := make([]string, 0)
	for _, record := range response.Answer {
		if address, ok := record.(*dns.A); ok {
			addresses = append(addresses, address.A.String())
		}
	}
	return addresses
}

// authorityNameServers returns only NS records from the Authority section.
func authorityNameServers(response *dns.Msg) []*dns.NS {
	if response == nil {
		return nil
	}
	nameServers := make([]*dns.NS, 0)
	for _, record := range response.Ns {
		if nameServer, ok := record.(*dns.NS); ok {
			nameServers = append(nameServers, nameServer)
		}
	}
	return nameServers
}

// isTerminalNegative preserves the legacy SOA termination heuristic without rewriting Rcode.
func isTerminalNegative(response *dns.Msg, qname string, qtype uint16) bool {
	if response == nil {
		return false
	}
	if response.Rcode == dns.RcodeNameError {
		return true
	}
	if qtype == dns.TypeSOA || len(response.Answer) != 0 {
		return false
	}
	canonicalName := dns.Fqdn(qname)
	for _, record := range response.Ns {
		soa, ok := record.(*dns.SOA)
		if !ok {
			continue
		}
		owner := dns.Fqdn(soa.Hdr.Name)
		if canonicalName != owner && strings.HasSuffix(canonicalName, owner) {
			return true
		}
	}
	return false
}

func tldPlusOne(name string) string {
	domain, err := publicsuffix.EffectiveTLDPlusOne(strings.TrimSuffix(name, "."))
	if err != nil {
		return name
	}
	return domain
}

type RecursiveResolver struct{}

func (resolver *RecursiveResolver) Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	return RecursiveLookup(ctx, name, qtype)
}

type FallbackResolver struct {
	primary   ILookup
	secondary ILookup
}

func NewFallbackResolver(primary ILookup, secondary ILookup) ILookup {
	return &FallbackResolver{primary: primary, secondary: secondary}
}

func (resolver *FallbackResolver) Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	response, err := normalizeLookupResult(resolver.primary.Lookup(ctx, name, qtype))
	if err != nil {
		return normalizeLookupResult(resolver.secondary.Lookup(ctx, name, qtype))
	}
	return response, nil
}
