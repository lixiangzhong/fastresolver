package fastresolver

import "context"

func NewRetryResolver(try int, r Resolver) Resolver {
	return &RetryResolver{
		retry:    try,
		resolver: r,
	}
}

var _ Resolver = (*RetryResolver)(nil)

type RetryResolver struct {
	retry    int
	resolver Resolver
}

func (r *RetryResolver) LookupIP(ctx context.Context, name string) (ret []string, err error) {
	for i := 0; i < r.retry; i++ {
		ret, err = r.resolver.LookupIP(ctx, name)
		if err == nil {
			return
		}
	}
	return
}

func (r *RetryResolver) LookupNS(ctx context.Context, name string) (ret []string, err error) {
	for i := 0; i < r.retry; i++ {
		ret, err = r.resolver.LookupNS(ctx, name)
		if err == nil {
			return
		}
	}
	return
}
