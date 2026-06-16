package fastresolver

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestDoH_Lookup(t *testing.T) {
	server := startMockDoHServer(func(w dns.ResponseWriter, r *dns.Msg) {
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
	defer server.Close()

	r := NewDoH(server.URL, time.Second*3)
	rr, err := r.Lookup(context.Background(), "cl.app", dns.TypeNS)
	if err != nil {
		t.Fatal(err)
	}

	if len(rr.NS) == 0 || rr.NS[0] != "ns1.mock.dns." {
		t.Fatalf("expected NS 'ns1.mock.dns.', got %v", rr.NS)
	}
}

type trackingRoundTripper struct {
	called bool
}

func (tr *trackingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.called = true
	return http.DefaultTransport.RoundTrip(req)
}

func TestDoH_WithCustomClient(t *testing.T) {
	server := startMockDoHServer(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		_ = w.WriteMsg(m)
	})
	defer server.Close()

	tr := &trackingRoundTripper{}
	client := &http.Client{
		Transport: tr,
		Timeout:   time.Second * 5,
	}
	r := NewDoHWithClient(server.URL, client)
	_, _ = r.Lookup(context.Background(), "cl.app", dns.TypeNS)

	if !tr.called {
		t.Error("expected custom client's RoundTrip to be called, but it was not")
	}
}
