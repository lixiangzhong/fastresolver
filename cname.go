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
	return &FollowCnameResolver{
		resolver: resolver,
		maxDepth: defaultCnameFollowMaxDepth,
	}
}

// Lookup implements ILookup.
func (f *FollowCnameResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	return f.lookup(ctx, name, qtype, 0)
}

func (f *FollowCnameResolver) lookup(ctx context.Context, name string, qtype uint16, depth int) (DNSRR, error) {
	ret, err := f.resolver.Lookup(ctx, name, qtype)
	if err != nil {
		return ret, err
	}
	if qtype == dns.TypeNS {
		return ret, nil
	}
	if len(ret.CNAME) > 0 {
		var follow bool
		switch qtype {
		case dns.TypeA:
			follow = len(ret.A) == 0
		case dns.TypeAAAA:
			follow = len(ret.AAAA) == 0
		case dns.TypePTR:
			follow = len(ret.PTR) == 0
		}
		if follow {
			if depth >= f.maxDepth {
				return ret, fmt.Errorf("%w: %s", ErrCnameDepthExceeded, name)
			}
			return f.lookup(ctx, ret.CNAME[0], qtype, depth+1)
		}
	}
	return ret, err
}
