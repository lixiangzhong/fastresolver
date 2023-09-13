package fastresolver

import (
	"context"
	"math/rand"
	"net"
	"net/netip"
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
	_, err := netip.ParseAddr(u.Addr)
	if err == nil {
		return net.Dial(u.Network, u.Addr+":"+strconv.Itoa(u.Port))
	}
	addrs, err := DefaultResovler.LookupIP(context.Background(), u.Addr)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, &net.DNSError{
			Err:        "no such host",
			Name:       u.Addr,
			IsNotFound: true,
		}
	}
	idx := rand.Intn(len(addrs))
	return net.Dial(u.Network, addrs[idx]+":"+strconv.Itoa(u.Port))
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
	if addr, err := netip.ParseAddr(name); err == nil {
		return []string{addr.String()}, nil
	}
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
