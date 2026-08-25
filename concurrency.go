package fastresolver

import (
	"context"
	"errors"

	"github.com/miekg/dns"
)

type ConcurrencyResolver struct {
	resolvers []ILookup
}

func NewConcurrencyResolver(resolvers ...ILookup) *ConcurrencyResolver {
	return &ConcurrencyResolver{resolvers: resolvers}
}

type concurrentResult struct {
	response *dns.Msg
	err      error
}

func (resolver *ConcurrencyResolver) Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(resolver.resolvers) == 0 {
		return nil, ErrNoResolver
	}

	lookupCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan concurrentResult, len(resolver.resolvers))
	for _, candidate := range resolver.resolvers {
		go func(candidate ILookup) {
			response, err := normalizeLookupResult(candidate.Lookup(lookupCtx, name, qtype))
			results <- concurrentResult{response: response, err: err}
		}(candidate)
	}

	var failures []error
	var failedResponse *dns.Msg
	for range resolver.resolvers {
		select {
		case <-ctx.Done():
			return failedResponse, ctx.Err()
		case result := <-results:
			if result.err == nil {
				cancel()
				return result.response, nil
			}
			failures = append(failures, result.err)
			if failedResponse == nil && result.response != nil {
				failedResponse = result.response
			}
		}
	}
	return failedResponse, errors.Join(failures...)
}
