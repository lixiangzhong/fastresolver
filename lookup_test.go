package fastresolver

import (
	"context"
	"errors"
	"testing"

	"github.com/miekg/dns"
)

func TestResolver_Lookup(t *testing.T) {
	// Start local mock UDP DNS server
	server, err := startMockUDPServer(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Authoritative = true

		if r.Question[0].Name == "cl.app." && r.Question[0].Qtype == dns.TypeNS {
			ns := &dns.NS{
				Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 300},
				Ns:  "ns1.mock.dns.",
			}
			m.Answer = append(m.Answer, ns)
		}

		_ = w.WriteMsg(m)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()

	r, err := NewResolver(server.PacketConn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	rr, err := r.Lookup(context.Background(), "cl.app", dns.TypeNS)
	if err != nil {
		t.Fatal(err)
	}

	if len(rr.NS) == 0 || rr.NS[0] != "ns1.mock.dns." {
		t.Fatalf("expected NS 'ns1.mock.dns.', got %v", rr.NS)
	}
}

func TestToDNSRR_MinTTLWithZero(t *testing.T) {
	// 启动一个 Mock DNS 服务，其返回两个 NS 记录，一个 TTL 是 0，另一个 TTL 是 300
	server, err := startMockUDPServer(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Authoritative = true

		if r.Question[0].Name == "example.com." && r.Question[0].Qtype == dns.TypeNS {
			ns1 := &dns.NS{
				Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 0},
				Ns:  "ns1.mock.dns.",
			}
			ns2 := &dns.NS{
				Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 300},
				Ns:  "ns2.mock.dns.",
			}
			m.Answer = append(m.Answer, ns1, ns2)
		}

		_ = w.WriteMsg(m)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()

	r, err := NewResolver(server.PacketConn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	rr, err := r.Lookup(context.Background(), "example.com", dns.TypeNS)
	if err != nil {
		t.Fatal(err)
	}

	if rr.TTL != 0 {
		t.Fatalf("expected TTL to be 0, but got %v", rr.TTL)
	}
}

func TestToDNSRR_EmptyQuestion(t *testing.T) {
	// 上游返回无 Question 段的畸形响应时，toDNSRR 应返回 ErrNoQuestion 而非越界 panic。
	resp := new(dns.Msg)
	resp.Rcode = dns.RcodeSuccess

	var dnsrr DNSRR
	err := toDNSRR(resp, &dnsrr)
	if !errors.Is(err, ErrNoQuestion) {
		t.Fatalf("expected ErrNoQuestion, got %v", err)
	}
}
