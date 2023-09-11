package fastresolver

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/zonedb/zonedb"
	"golang.org/x/exp/slices"
	"golang.org/x/net/publicsuffix"
)

type Resolver interface {
	LookupIP(context.Context, string) ([]string, error)
	LookupNS(context.Context, string) ([]string, error)
	// Trace(context.Context, string, uint16) ([]string, error)
}

type DNSRR struct {
	ServerAddr    string
	NXDomain      bool
	Authoritative bool
	A             []string
	NS            []string
	CNAME         []string
	AuthNS        []string
	ExtraA        []string
	ExtraAAAA     []string
}

func lookup(ctx context.Context, conn net.Conn, name string, qtype uint16) (DNSRR, error) {
	var ret DNSRR
	ret.ServerAddr = conn.RemoteAddr().String()
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	c := new(dns.Client)
	c.DialTimeout = 3 * time.Second
	c.ReadTimeout = 5 * time.Second
	rsp, _, err := c.ExchangeWithConnContext(ctx, m, &dns.Conn{Conn: conn})
	if err != nil {
		return ret, err
	}
	if rsp.Truncated {
		//try again with TCP
		udpconn, ok := conn.(*net.UDPConn)
		if ok {
			tcpconn, err := net.DialTimeout("tcp", udpconn.RemoteAddr().String(), 3*time.Second)
			if err != nil {
				return ret, err
			}
			return lookup(ctx, tcpconn, name, qtype)
		}
		return ret, TruncatedError{Qname: name, Server: conn.RemoteAddr().String()}
	}
	ret.Authoritative = rsp.Authoritative
	if rsp.Rcode == dns.RcodeRefused {
		return ret, ServerRefusedError{Qname: name, Server: conn.RemoteAddr().String()}
	}
	if rsp.Rcode == dns.RcodeNameError {
		ret.NXDomain = true
		return ret, nil
	}
	for _, item := range rsp.Ns {
		switch v := item.(type) {
		case *dns.NS:
			ret.AuthNS = append(ret.AuthNS, v.Ns)
		case *dns.SOA:
			ret.NXDomain = true
			return ret, nil
		}
	}
	for _, item := range rsp.Answer {
		switch v := item.(type) {
		case *dns.A:
			ret.A = append(ret.A, v.A.String())
		case *dns.NS:
			ret.NS = append(ret.NS, v.Ns)
		case *dns.CNAME:
			ret.CNAME = append(ret.CNAME, v.Target)
		}
	}
	for _, item := range rsp.Extra {
		switch v := item.(type) {
		case *dns.A:
			ret.ExtraA = append(ret.ExtraA, v.A.String())
		case *dns.AAAA:
			ret.ExtraAAAA = append(ret.ExtraAAAA, v.AAAA.String())
		}
	}
	// fmt.Println(conn.RemoteAddr(), name, qtype, ret)
	return ret, nil
}

func trace(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	var nxdomain DNSRR = DNSRR{NXDomain: true}
	var upstreams Upstreams
	z := zonedb.PublicZone(tldPlusOne(name))
	if z == nil {
		return nxdomain, nil
	}
	for _, ns := range z.NameServers {
		upstreams = append(upstreams, Upstream{
			Addr: ns,
		})
	}
	if len(upstreams) == 0 {
		upstreams = slices.Clone(roots)
	}
	for i := 0; i < 16; i++ {
		rsp, err := upstreams.Lookup(ctx, name, qtype)
		if err != nil {
			continue
		}
		if rsp.Authoritative || rsp.NXDomain {
			return rsp, nil
		}
		if len(rsp.AuthNS) > 0 {
			upstreams = upstreams[:0]
			for _, ns := range rsp.AuthNS {
				upstreams = append(upstreams, Upstream{
					Addr: ns,
				})
			}
			continue
		}
		break
	}
	return nxdomain, nil
}

func tldPlusOne(name string) string {
	domain, err := publicsuffix.EffectiveTLDPlusOne(strings.TrimSuffix(name, "."))
	if err != nil {
		return name
	}
	return domain
}
