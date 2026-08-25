package fastresolver

import (
	"context"
	"fmt"

	"github.com/miekg/dns"
)

// defaultCnameFollowMaxDepth limits recursive CNAME lookups to prevent cycles.
const defaultCnameFollowMaxDepth = 16

type FollowCnameResolver struct {
	resolver ILookup
	maxDepth int
}

func NewFollowCnameResolver(resolver ILookup) *FollowCnameResolver {
	return &FollowCnameResolver{resolver: resolver, maxDepth: defaultCnameFollowMaxDepth}
}

// Lookup implements ILookup.
func (resolver *FollowCnameResolver) Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	return resolver.lookup(ctx, name, qtype, 0)
}

func (resolver *FollowCnameResolver) lookup(ctx context.Context, name string, qtype uint16, depth int) (*dns.Msg, error) {
	response, err := normalizeLookupResult(resolver.resolver.Lookup(ctx, name, qtype))
	if err != nil {
		return response, err
	}
	if qtype == dns.TypeNS || !isFollowedCNAMEType(qtype) {
		return response, nil
	}

	var cname *dns.CNAME
	hasTargetType := false
	for _, record := range response.Answer {
		if record.Header().Rrtype == qtype {
			hasTargetType = true
		}
		if cname == nil {
			cname, _ = record.(*dns.CNAME)
		}
	}
	if cname == nil || hasTargetType {
		return response, nil
	}
	if depth >= resolver.maxDepth {
		return response, fmt.Errorf("%w: %s", ErrCnameDepthExceeded, name)
	}
	return resolver.lookup(ctx, cname.Target, qtype, depth+1)
}

// isFollowedCNAMEType keeps automatic following limited to the v2 query types.
func isFollowedCNAMEType(qtype uint16) bool {
	return qtype == dns.TypeA || qtype == dns.TypeAAAA || qtype == dns.TypePTR
}

// Unwrap returns the underlying resolver.
func (resolver *FollowCnameResolver) Unwrap() ILookup {
	return resolver.resolver
}
