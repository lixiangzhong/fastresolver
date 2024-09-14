package fastresolver

import (
	"context"

	"github.com/miekg/dns"
)

type FollowCnameResolver struct {
	resolver ILookup
}

func NewFollowCnameResolver(resolver ILookup) *FollowCnameResolver {
	return &FollowCnameResolver{
		resolver: resolver,
	}
}

// Lookup implements ILookup.
func (f *FollowCnameResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
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
			return f.Lookup(ctx, ret.CNAME[0], qtype)
		}
	}
	return ret, err
}
