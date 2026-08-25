package fastresolver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestDoH_LookupNativeResponse(t *testing.T) {
	requestIDs := make(chan uint16, 1)
	server := startMockDoHServer(func(writer dns.ResponseWriter, request *dns.Msg) {
		requestIDs <- request.Id
		response := new(dns.Msg).SetReply(request)
		response.Authoritative = true
		response.RecursionAvailable = true
		response.AuthenticatedData = true
		response.Answer = []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: request.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   net.ParseIP("192.0.2.10"),
		}}
		response.Ns = []dns.RR{&dns.NS{
			Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 300},
			Ns:  "ns1.example.com.",
		}}
		response.Extra = []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: "ns1.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   net.ParseIP("192.0.2.53"),
		}}
		response.SetEdns0(1232, true)
		_ = writer.WriteMsg(response)
	})
	defer server.Close()

	resolver := NewDoH(server.URL, 3*time.Second)
	response, err := resolver.Lookup(context.Background(), "www.example.com", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	if requestID := <-requestIDs; requestID != 0 {
		t.Fatalf("got DoH request ID %d, want 0", requestID)
	}
	if response.Id == 0 {
		t.Fatal("caller-facing response ID was not restored")
	}
	if !response.Authoritative || !response.RecursionAvailable || !response.AuthenticatedData {
		t.Fatalf("response flags were not preserved: %+v", response.MsgHdr)
	}
	if len(response.Answer) != 1 || len(response.Ns) != 1 || len(response.Extra) != 2 || response.IsEdns0() == nil {
		t.Fatalf("response sections were not preserved: %+v", response)
	}
}

func TestDoH_LookupRejectsNonzeroResponseID(t *testing.T) {
	server := startMockDoHServer(func(writer dns.ResponseWriter, request *dns.Msg) {
		response := new(dns.Msg).SetReply(request)
		response.Id = 42
		_ = writer.WriteMsg(response)
	})
	defer server.Close()

	response, err := NewDoH(server.URL, time.Second).Lookup(context.Background(), "example.com", dns.TypeA)
	if response == nil || response.Id != 42 || !errors.Is(err, ErrInvalidResponseID) {
		t.Fatalf("got response=%v error=%v, want nonzero response and ErrInvalidResponseID", response, err)
	}
}

func TestDoH_LookupRefusedReturnsResponse(t *testing.T) {
	server := startMockDoHServer(func(writer dns.ResponseWriter, request *dns.Msg) {
		_ = writer.WriteMsg(new(dns.Msg).SetRcode(request, dns.RcodeRefused))
	})
	defer server.Close()

	response, err := NewDoH(server.URL, time.Second).Lookup(context.Background(), "example.com", dns.TypeA)
	var refused ServerRefusedError
	if response == nil || response.Rcode != dns.RcodeRefused || !errors.As(err, &refused) {
		t.Fatalf("got response=%v error=%v, want refused response and typed error", response, err)
	}
}

func TestDoH_LookupHTTPAndWireErrors(t *testing.T) {
	t.Run("http status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			http.Error(writer, "unavailable", http.StatusServiceUnavailable)
		}))
		defer server.Close()

		response, err := NewDoH(server.URL, time.Second).Lookup(context.Background(), "example.com", dns.TypeA)
		if response != nil || err == nil {
			t.Fatalf("got response=%v error=%v, want nil response and HTTP error", response, err)
		}
	})

	t.Run("malformed body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/dns-message")
			_, _ = writer.Write([]byte("not a dns message"))
		}))
		defer server.Close()

		response, err := NewDoH(server.URL, time.Second).Lookup(context.Background(), "example.com", dns.TypeA)
		if response != nil || err == nil {
			t.Fatalf("got response=%v error=%v, want nil response and decode error", response, err)
		}
	})
}

type trackingRoundTripper struct {
	called bool
}

func (transport *trackingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.called = true
	return http.DefaultTransport.RoundTrip(request)
}

func TestDoH_WithCustomClient(t *testing.T) {
	server := startMockDoHServer(func(writer dns.ResponseWriter, request *dns.Msg) {
		_ = writer.WriteMsg(new(dns.Msg).SetReply(request))
	})
	defer server.Close()

	transport := &trackingRoundTripper{}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	resolver := NewDoHWithClient(server.URL, client)
	_, _ = resolver.Lookup(context.Background(), "example.com", dns.TypeA)

	if !transport.called {
		t.Error("expected custom client's RoundTrip to be called")
	}
}
