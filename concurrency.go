package fastresolver

import (
	"context"
	"errors"
	"sync"
)

type ConcurrencyResolver struct {
	resolvers []ILookup
}

func NewConcurrencyResolver(resolvers ...ILookup) *ConcurrencyResolver {
	return &ConcurrencyResolver{
		resolvers: resolvers,
	}
}

func (r *ConcurrencyResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	result := make(chan DNSRR, len(r.resolvers))
	errch := make(chan error, len(r.resolvers))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	wg := &sync.WaitGroup{}
	wg.Add(len(r.resolvers))
	for _, r := range r.resolvers {
		go func(r ILookup) {
			defer wg.Done()
			rr, err := r.Lookup(ctx, name, qtype)
			select {
			case <-ctx.Done():
				return
			default:
				if err == nil {
					result <- rr
				} else {
					errch <- err
				}
			}
		}(r)
	}
	go func() {
		wg.Wait()
		close(result)
		close(errch)
	}()
	var errs []error
	for range r.resolvers {
		select {
		case err := <-errch:
			errs = append(errs, err)
		case rr := <-result:
			return rr, nil
		}
	}
	return DNSRR{}, errors.Join(errs...)
}
