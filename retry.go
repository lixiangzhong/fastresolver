package fastresolver

import "context"

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
func (r *RetryResolver) Lookup(ctx context.Context, name string, qtype uint16) (ret DNSRR, err error) {
	for i := 0; i < r.retry; i++ {
		if err = ctx.Err(); err != nil {
			return DNSRR{}, err
		}
		ret, err = r.resolver.Lookup(ctx, name, qtype)
		if err == nil {
			return ret, err
		}
	}
	return
}

// Unwrap returns the underlying resolver.
func (r *RetryResolver) Unwrap() ILookup {
	return r.resolver
}
