package fastresolver

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

type ILookup interface {
	Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error)
}

type DNSRR struct {
	ServerAddr    string
	NXDomain      bool
	Authoritative bool
	Rtt           time.Duration
	Network       string
	A             []string
	AAAA          []string
	NS            []string
	CNAME         []string
	AuthNS        []AuthNS
	PTR           []string
	MX            []MX
	TXT           []string
	SRV           []string
}

type AuthNS struct {
	Name  string
	Value string
}
type MX struct {
	Preference uint16
	Value      string
}

var _ ILookup = (*Resolver)(nil)

func NewResolver(server string) (*Resolver, error) {
	host, port, err := net.SplitHostPort(server)
	if err != nil {
		if !strings.Contains(err.Error(), "missing port in address") {
			return nil, err
		}
		host = server
		port = "53"
	}
	return &Resolver{
		server: net.JoinHostPort(host, port),
		udp: &dns.Client{
			Net:     "udp",
			Timeout: 3 * time.Second,
		},
		tcp: &dns.Client{
			Net:     "tcp",
			Timeout: 3 * time.Second,
		},
	}, nil
}

type Resolver struct {
	server string
	udp    *dns.Client
	tcp    *dns.Client
}

// Lookup implements ILookup.
func (r *Resolver) Lookup(ctx context.Context, name string, qtype uint16) (resp DNSRR, err error) {
	resp, err = r.exchange(ctx, new(dns.Msg).SetQuestion(dns.Fqdn(name), qtype))
	if err != nil {
		return resp, err
	}
	return
}

func (r *Resolver) exchange(ctx context.Context, req *dns.Msg) (dnsrr DNSRR, err error) {
	dnsrr.Network = "udp"
	conn, err := r.udp.DialContext(ctx, r.server)
	if err != nil {
		return
	}
	defer conn.Close()
	dnsrr.ServerAddr = conn.RemoteAddr().String()
	resp, rtt, err := r.udp.ExchangeWithConnContext(ctx, req, conn)
	if err != nil {
		return
	}
	if resp.Truncated {
		tcpconn, err := r.tcp.DialContext(ctx, r.server)
		if err != nil {
			return dnsrr, err
		}
		defer tcpconn.Close()
		resp, rtt, err = r.tcp.ExchangeWithConnContext(ctx, req, tcpconn)
		if err != nil {
			return dnsrr, err
		}
		dnsrr.Network = "tcp"
		dnsrr.ServerAddr = tcpconn.RemoteAddr().String()
	}
	dnsrr.Rtt = rtt
	err = toDNSRR(resp, &dnsrr)
	if err != nil {
		return
	}
	return
}

func toDNSRR(resp *dns.Msg, dnsrr *DNSRR) (err error) {
	qtype := resp.Question[0].Qtype
	qname := resp.Question[0].Name
	dnsrr.Authoritative = resp.Authoritative
	switch resp.Rcode {
	case dns.RcodeRefused:
		err = ServerRefusedError{Qname: qname, Server: dnsrr.ServerAddr}
		return
	case dns.RcodeNameError:
		dnsrr.NXDomain = true
		return
	}
	for _, v := range resp.Ns {
		switch rr := v.(type) {
		case *dns.NS:
			dnsrr.AuthNS = append(dnsrr.AuthNS, AuthNS{
				Name:  v.Header().Name,
				Value: rr.Ns,
			})
		case *dns.SOA:
			if qtype == dns.TypeSOA {
				continue
			}
			dnsrr.NXDomain = strings.HasSuffix(qname, v.Header().Name) && qname != v.Header().Name && len(resp.Answer) == 0
		}
	}
	for _, v := range resp.Answer {
		switch rr := v.(type) {
		case *dns.A:
			dnsrr.A = append(dnsrr.A, rr.A.String())
		case *dns.AAAA:
			dnsrr.AAAA = append(dnsrr.AAAA, rr.AAAA.String())
		case *dns.NS:
			dnsrr.NS = append(dnsrr.NS, rr.Ns)
		case *dns.CNAME:
			dnsrr.CNAME = append(dnsrr.CNAME, rr.Target)
		case *dns.PTR:
			dnsrr.PTR = append(dnsrr.PTR, rr.Ptr)
		case *dns.TXT:
			for _, txt := range rr.Txt {
				dnsrr.TXT = append(dnsrr.TXT, txt)
			}
		case *dns.MX:
			dnsrr.MX = append(dnsrr.MX, MX{
				Preference: rr.Preference,
				Value:      rr.Mx,
			})
		case *dns.SRV:
			dnsrr.SRV = append(dnsrr.SRV, fmt.Sprintf("%v %v %v %v", rr.Priority, rr.Weight, rr.Port, rr.Target))
		}
	}
	return
}
