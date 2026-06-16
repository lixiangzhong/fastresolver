package fastresolver

import (
	"context"
	"net"
	"testing"

	"github.com/miekg/dns"
)

func TestConcurrencyResolver_Lookup(t *testing.T) {
	handler := func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Authoritative = true
		if r.Question[0].Name == "example.com." && r.Question[0].Qtype == dns.TypeA {
			rr := &dns.A{
				Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
				A:   net.ParseIP("127.0.0.1"),
			}
			m.Answer = append(m.Answer, rr)
		}
		_ = w.WriteMsg(m)
	}

	server1, err := startMockUDPServer(handler)
	if err != nil {
		t.Fatal(err)
	}
	defer server1.Shutdown()

	server2, err := startMockUDPServer(handler)
	if err != nil {
		t.Fatal(err)
	}
	defer server2.Shutdown()

	r1, err := NewResolver(server1.PacketConn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	r2, err := NewResolver(server2.PacketConn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	// bad server (a non-existent local port to make it fail quickly or silently)
	r3, err := NewResolver("127.0.0.1:53000")
	if err != nil {
		t.Fatal(err)
	}

	r := NewConcurrencyResolver(r1, r2, r3)
	ret, err := r.Lookup(context.Background(), "example.com", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	if len(ret.A) == 0 || ret.A[0] != "127.0.0.1" {
		t.Fatalf("expected A '127.0.0.1', got %v", ret.A)
	}
}
