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
