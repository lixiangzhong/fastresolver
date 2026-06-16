package fastresolver

import (
	"context"
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
