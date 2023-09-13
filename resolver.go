package fastresolver

import (
	"context"
	"net"
	"time"

	"github.com/miekg/dns"
)

type Resolver interface {
	LookupIP(context.Context, string) ([]string, error)
	LookupNS(context.Context, string) ([]string, error)
	Lookup(context.Context, string, uint16) (DNSRR, error)
}

type DNSRR struct {
	ServerAddr    string
	NXDomain      bool
	Authoritative bool
	A             []string
	AAAA          []string
	NS            []string
	CNAME         []string
	AuthNS        []string
	//mx txt ...
}

func lookup(ctx context.Context, conn net.Conn, name string, qtype uint16) (ret DNSRR, err error) {
	defer func() {
		ret.ServerAddr = conn.RemoteAddr().String()
	}()
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
		case *dns.AAAA:
			ret.AAAA = append(ret.AAAA, v.AAAA.String())
		case *dns.NS:
			ret.NS = append(ret.NS, v.Ns)
		case *dns.CNAME:
			ret.CNAME = append(ret.CNAME, v.Target)
		}
	}
	return ret, nil
}
