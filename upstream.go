package fastresolver

import (
	"context"
	"net"
	"strconv"

	"github.com/miekg/dns"
)

var _ Resolver = (*Upstream)(nil)

type Upstream struct {
	Network string
	Addr    string
	Port    int
}

func (u Upstream) Dial() (net.Conn, error) {
	if u.Network == "" {
		u.Network = "udp"
	}
	if u.Port == 0 {
		u.Port = 53
	}
	return net.Dial(u.Network, u.Addr+":"+strconv.Itoa(u.Port))
}

func (u Upstream) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	var ret DNSRR
	conn, err := u.Dial()
	if err != nil {
		return ret, err
	}
	defer conn.Close()
	return lookup(ctx, conn, name, qtype)
}

func (u Upstream) LookupIP(ctx context.Context, name string) ([]string, error) {
	ret, err := u.Lookup(ctx, name, dns.TypeA)
	if err != nil {
		return nil, err
	}
	if len(ret.A) > 0 {
		return ret.A, nil
	}
	if len(ret.CNAME) > 0 {
		return u.LookupIP(ctx, ret.CNAME[0])
	}
	return nil, nil
}

func (u Upstream) LookupNS(ctx context.Context, name string) ([]string, error) {
	ret, err := u.Lookup(ctx, name, dns.TypeNS)
	if err != nil {
		return nil, err
	}
	if len(ret.NS) > 0 {
		return ret.NS, nil
	}
	if len(ret.CNAME) > 0 {
		return u.LookupNS(ctx, ret.CNAME[0])
	}
	return nil, nil
}
