package fastresolver

import (
	"context"

	"github.com/miekg/dns"
)

func NewRetryResolver(try int, r ILookup) ILookup {
	return &RetryResolver{
		retry:    try,
		resolver: r,
	}
}

var _ ILookup = (*RetryResolver)(nil)

type RetryResolver struct {
	retry    int
	resolver ILookup
}

// Lookup implements ILookup.
func (r *RetryResolver) Lookup(ctx context.Context, name string, qtype uint16) (response *dns.Msg, err error) {
	for attempt := 0; attempt < r.retry; attempt++ {
		if err = ctx.Err(); err != nil {
			return response, err
		}
		response, err = normalizeLookupResult(r.resolver.Lookup(ctx, name, qtype))
		if err == nil {
			return response, nil
		}
	}
	return normalizeLookupResult(response, err)
}

// Unwrap returns the underlying resolver.
func (r *RetryResolver) Unwrap() ILookup {
	return r.resolver
}
