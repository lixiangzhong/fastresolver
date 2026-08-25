package fastresolver

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestJSONAPI_LookupNativeResponse(t *testing.T) {
	server := startMockJSONAPIServer(func(name string, qtype uint16) JSONAPIResponse {
		return JSONAPIResponse{
			Status:   dns.RcodeSuccess,
			TC:       true,
			RD:       true,
			RA:       true,
			AD:       true,
			CD:       true,
			Question: JSONAPIQuestions{{Name: "example.com.", Type: dns.TypeA}},
			Answer: []JSONAPIAnswer{
				{Name: "example.com.", Type: dns.TypeA, TTL: 300, Data: "192.0.2.20"},
				{Name: "example.com.", Type: dns.TypeMX, TTL: 400, Data: "10 mail.example.com."},
				{Name: "example.com.", Type: dns.TypeCAA, TTL: 500, Data: `0 issue "letsencrypt.org"`},
				{Name: "example.com.", Type: 65400, TTL: 600, Data: `\# 2 CAFE`},
			},
			Authority: []JSONAPIAnswer{{
				Name: "example.com.", Type: dns.TypeSOA, TTL: 60,
				Data: "ns1.example.com. hostmaster.example.com. 1 2 3 4 5",
			}},
		}
	})
	defer server.Close()

	response, err := NewJSONAPI(server.URL, 3*time.Second).Lookup(context.Background(), "example.com", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	if !response.Response || response.Rcode != dns.RcodeSuccess || !response.Truncated || !response.RecursionDesired || !response.RecursionAvailable || !response.AuthenticatedData || !response.CheckingDisabled {
		t.Fatalf("response header was not mapped: %+v", response.MsgHdr)
	}
	if len(response.Question) != 1 || len(response.Answer) != 4 || len(response.Ns) != 1 || len(response.Extra) != 0 {
		t.Fatalf("response sections were not mapped: %+v", response)
	}
	if _, ok := firstRR[*dns.A](response.Answer); !ok {
		t.Fatalf("missing A record: %v", response.Answer)
	}
	if _, ok := firstRR[*dns.MX](response.Answer); !ok {
		t.Fatalf("missing MX record: %v", response.Answer)
	}
	if _, ok := firstRR[*dns.CAA](response.Answer); !ok {
		t.Fatalf("missing CAA record: %v", response.Answer)
	}
	if _, ok := firstRR[*dns.RFC3597](response.Answer); !ok {
		t.Fatalf("missing RFC3597 record: %v", response.Answer)
	}
	if _, ok := firstRR[*dns.SOA](response.Ns); !ok {
		t.Fatalf("missing SOA authority record: %v", response.Ns)
	}
}

func TestJSONAPI_LookupRcodeContract(t *testing.T) {
	tests := []struct {
		name      string
		rcode     int
		wantError bool
	}{
		{name: "nxdomain", rcode: dns.RcodeNameError},
		{name: "servfail", rcode: dns.RcodeServerFailure},
		{name: "refused", rcode: dns.RcodeRefused, wantError: true},
		{name: "nodata", rcode: dns.RcodeSuccess},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := startMockJSONAPIServer(func(name string, qtype uint16) JSONAPIResponse {
				return JSONAPIResponse{
					Status:   test.rcode,
					Question: JSONAPIQuestions{{Name: "missing.example.com.", Type: dns.TypeA}},
					Authority: []JSONAPIAnswer{{
						Name: "example.com.", Type: dns.TypeSOA, TTL: 60,
						Data: "ns1.example.com. hostmaster.example.com. 1 2 3 4 5",
					}},
				}
			})
			defer server.Close()

			response, err := NewJSONAPI(server.URL, time.Second).Lookup(context.Background(), "missing.example.com", dns.TypeA)
			if response == nil || response.Rcode != test.rcode {
				t.Fatalf("got response=%v, want rcode %d", response, test.rcode)
			}
			if test.wantError {
				var refused ServerRefusedError
				if !errors.As(err, &refused) {
					t.Fatalf("got error %v, want ServerRefusedError", err)
				}
			} else if err != nil {
				t.Fatalf("got unexpected error %v", err)
			}
		})
	}
}

func TestJSONAPI_LookupRejectsInvalidResponse(t *testing.T) {
	tests := []struct {
		name     string
		response JSONAPIResponse
		want     error
	}{
		{
			name:     "missing question",
			response: JSONAPIResponse{Status: dns.RcodeSuccess},
			want:     ErrNoQuestion,
		},
		{
			name: "mismatched question",
			response: JSONAPIResponse{
				Status:   dns.RcodeSuccess,
				Question: JSONAPIQuestions{{Name: "other.example.", Type: dns.TypeA}},
			},
			want: ErrInvalidQuestion,
		},
		{
			name: "invalid rr",
			response: JSONAPIResponse{
				Status:   dns.RcodeSuccess,
				Question: JSONAPIQuestions{{Name: "example.com.", Type: dns.TypeA}},
				Answer:   []JSONAPIAnswer{{Name: "example.com.", Type: dns.TypeA, TTL: 60, Data: "not-an-ip"}},
			},
		},
		{
			name: "newline injection",
			response: JSONAPIResponse{
				Status:   dns.RcodeSuccess,
				Question: JSONAPIQuestions{{Name: "example.com.", Type: dns.TypeTXT}},
				Answer:   []JSONAPIAnswer{{Name: "example.com.\nmalicious.example.", Type: dns.TypeTXT, TTL: 60, Data: `"value"`}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := startMockJSONAPIServer(func(name string, qtype uint16) JSONAPIResponse { return test.response })
			defer server.Close()

			response, err := NewJSONAPI(server.URL, time.Second).Lookup(context.Background(), "example.com", dns.TypeA)
			if response == nil || err == nil {
				t.Fatalf("got response=%v error=%v, want partial response and error", response, err)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("got error %v, want %v", err, test.want)
			}
		})
	}
}

func TestJSONAPI_WithCustomClient(t *testing.T) {
	server := startMockJSONAPIServer(func(name string, qtype uint16) JSONAPIResponse {
		return JSONAPIResponse{
			Status:   dns.RcodeSuccess,
			Question: JSONAPIQuestions{{Name: "example.com.", Type: dns.TypeA}},
		}
	})
	defer server.Close()

	transport := &trackingRoundTripper{}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	resolver := NewJSONAPIWithClient(server.URL, client)
	_, _ = resolver.Lookup(context.Background(), "example.com", dns.TypeA)

	if !transport.called {
		t.Error("expected custom client's RoundTrip to be called")
	}
}
