package fastresolver

import (
	"context"
	"math/rand"
	"time"
)

func NewFailoverResovler(threshold uint64, r ...Resolver) Resolver {
	upstreams := make([]StatefulResolver, 0, len(r))
	for _, u := range r {
		upstreams = append(upstreams, NewStatefulResolver(u, threshold))
	}
	return &failoverResolver{upstreams: upstreams}
}

var _ Resolver = (*failoverResolver)(nil)

type failoverResolver struct {
	upstreams []StatefulResolver
}

func (s *failoverResolver) LookupIP(ctx context.Context, name string) ([]string, error) {
	idx := s.take()
	u := s.upstreams[idx]
	ret, err := u.LookupIP(ctx, name)
	if err != nil {
		u.Record(false)
	} else {
		u.Record(true)
	}
	return ret, err
}

func (s *failoverResolver) LookupNS(ctx context.Context, name string) ([]string, error) {
	idx := s.take()
	u := s.upstreams[idx]
	ret, err := u.LookupNS(ctx, name)
	if err != nil {
		u.Record(false)
	} else {
		u.Record(true)
	}
	return ret, err
}

func (s *failoverResolver) take() int {
	n := len(s.upstreams)
	available := make([]int, 0, n)
	for i, u := range s.upstreams {
		if u.Available() {
			available = append(available, i)
		}
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	if len(available) == 0 {
		return r.Intn(n)
	}
	return available[r.Intn(len(available))]
}
