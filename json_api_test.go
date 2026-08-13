package fastresolver

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestJSONAPI_Lookup(t *testing.T) {
	server := startMockJSONAPIServer(func(name string, qtype uint16) JSONAPIResponse {
		if name == "cl.app" && qtype == dns.TypeNS {
			return JSONAPIResponse{
				Status: dns.RcodeSuccess,
				Answer: []JSONAPIAnswer{
					{
						Name: "cl.app.",
						Type: dns.TypeNS,
						TTL:  300,
						Data: "ns1.mock.dns.",
					},
				},
			}
		}
		return JSONAPIResponse{Status: dns.RcodeNameError}
	})
	defer server.Close()

	r := NewJSONAPI(server.URL, time.Second*3)
	rr, err := r.Lookup(context.Background(), "cl.app", dns.TypeNS)
	if err != nil {
		t.Fatal(err)
	}

	if len(rr.NS) == 0 || rr.NS[0] != "ns1.mock.dns." {
		t.Fatalf("expected NS 'ns1.mock.dns.', got %v", rr.NS)
	}
}

func TestJSONAPI_WithCustomClient(t *testing.T) {
	server := startMockJSONAPIServer(func(name string, qtype uint16) JSONAPIResponse {
		return JSONAPIResponse{Status: dns.RcodeSuccess}
	})
	defer server.Close()

	tr := &trackingRoundTripper{}
	client := &http.Client{
		Transport: tr,
		Timeout:   time.Second * 5,
	}
	r := NewJSONAPIWithClient(server.URL, client)
	_, _ = r.Lookup(context.Background(), "cl.app", dns.TypeNS)

	if !tr.called {
		t.Error("expected custom client's RoundTrip to be called, but it was not")
	}
}

func TestJSONAPI_SOA(t *testing.T) {
	server := startMockJSONAPIServer(func(name string, qtype uint16) JSONAPIResponse {
		return JSONAPIResponse{
			Status: dns.RcodeSuccess,
			Answer: []JSONAPIAnswer{{
				Name: "example.com.", Type: dns.TypeSOA, TTL: 300,
				Data: "ns1.example.com. hostmaster.example.com. 2026081301 3600 600 86400 300",
			}},
		}
	})
	defer server.Close()

	r := NewJSONAPI(server.URL, time.Second*3)
	rr, err := r.Lookup(context.Background(), "example.com", dns.TypeSOA)
	if err != nil {
		t.Fatal(err)
	}
	if rr.SOA == nil || rr.SOA.MName != "ns1.example.com." || rr.SOA.Serial != 2026081301 {
		t.Fatalf("unexpected SOA: %+v", rr.SOA)
	}
}

func TestJSONAPI_SOAAuthorityOnNXDomain(t *testing.T) {
	server := startMockJSONAPIServer(func(name string, qtype uint16) JSONAPIResponse {
		return JSONAPIResponse{
			Status: dns.RcodeNameError,
			Authority: []JSONAPIAnswer{{
				Name: "example.com.", Type: dns.TypeSOA, TTL: 60,
				Data: "ns1.example.com. hostmaster.example.com. 1 2 3 4 5",
			}},
		}
	})
	defer server.Close()

	r := NewJSONAPI(server.URL, time.Second*3)
	rr, err := r.Lookup(context.Background(), "example.com", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	if !rr.NXDomain || rr.SOA == nil || rr.SOA.Minimum != 5 {
		t.Fatalf("expected NXDomain with SOA, got %+v", rr)
	}
}

func TestJSONAPI_SOAAuthorityForExistingNameDoesNotSetNXDomain(t *testing.T) {
	server := startMockJSONAPIServer(func(name string, qtype uint16) JSONAPIResponse {
		return JSONAPIResponse{
			Status: dns.RcodeSuccess,
			Authority: []JSONAPIAnswer{{
				Name: "251.36.in-addr.arpa.", Type: dns.TypeSOA, TTL: 592,
				Data: "ns1.fj133165.com. root.fj133165.com. 2013120303 21600 3600 604800 86400",
			}},
		}
	})
	defer server.Close()

	r := NewJSONAPI(server.URL, time.Second*3)
	rr, err := r.Lookup(context.Background(), "251.36.in-addr.arpa", dns.TypePTR)
	if err != nil {
		t.Fatal(err)
	}
	if rr.NXDomain {
		t.Fatalf("expected NODATA response, got %+v", rr)
	}
	if rr.SOA == nil {
		t.Fatal("expected SOA")
	}
}
