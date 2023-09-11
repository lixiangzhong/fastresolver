package fastresolver

import (
	"context"
	"math/rand"
	"time"
)

var _ Resolver = (*Upstreams)(nil)

type Upstreams []Upstream

func (u Upstreams) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return u[r.Intn(len(u))].Lookup(ctx, name, qtype)
}
func (u Upstreams) LookupIP(ctx context.Context, name string) ([]string, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return u[r.Intn(len(u))].LookupIP(ctx, name)
}

func (u Upstreams) LookupNS(ctx context.Context, name string) ([]string, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return u[r.Intn(len(u))].LookupNS(ctx, name)
}
