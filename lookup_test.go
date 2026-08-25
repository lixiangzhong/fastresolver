package fastresolver

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func TestResolver_LookupNativeResponse(t *testing.T) {
	server, err := startMockUDPServer(func(writer dns.ResponseWriter, request *dns.Msg) {
		response := new(dns.Msg).SetReply(request)
		response.Authoritative = true
		response.RecursionAvailable = true
		response.AuthenticatedData = true
		response.Answer = []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: request.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   net.ParseIP("192.0.2.1"),
		}}
		response.Ns = []dns.RR{&dns.NS{
			Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 600},
			Ns:  "ns1.example.com.",
		}}
		response.Extra = []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: "ns1.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 600},
			A:   net.ParseIP("192.0.2.53"),
		}}
		_ = writer.WriteMsg(response)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()

	resolver, err := NewResolver(server.PacketConn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	response, err := resolver.Lookup(context.Background(), "www.example.com", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	if !response.Authoritative || !response.RecursionAvailable || !response.AuthenticatedData {
		t.Fatalf("response flags were not preserved: %+v", response.MsgHdr)
	}
	if len(response.Question) != 1 || len(response.Answer) != 1 || len(response.Ns) != 1 || len(response.Extra) != 1 {
		t.Fatalf("response sections were not preserved: %+v", response)
	}
	if record, ok := firstRR[*dns.A](response.Answer); !ok || record.A.String() != "192.0.2.1" {
		t.Fatalf("unexpected answer: %v", response.Answer)
	}
}

func TestResolver_LookupRcodeContract(t *testing.T) {
	tests := []struct {
		name      string
		rcode     int
		wantError bool
	}{
		{name: "nxdomain", rcode: dns.RcodeNameError},
		{name: "servfail", rcode: dns.RcodeServerFailure},
		{name: "refused", rcode: dns.RcodeRefused, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, err := startMockUDPServer(func(writer dns.ResponseWriter, request *dns.Msg) {
				response := new(dns.Msg).SetRcode(request, test.rcode)
				_ = writer.WriteMsg(response)
			})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Shutdown()

			resolver, err := NewResolver(server.PacketConn.LocalAddr().String())
			if err != nil {
				t.Fatal(err)
			}
			response, err := resolver.Lookup(context.Background(), "example.com", dns.TypeA)
			if response == nil || response.Rcode != test.rcode {
				t.Fatalf("got response %+v, want rcode %d", response, test.rcode)
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

func TestResolver_LookupNoQuestion(t *testing.T) {
	server, err := startMockUDPServer(func(writer dns.ResponseWriter, request *dns.Msg) {
		response := new(dns.Msg)
		response.Id = request.Id
		response.Response = true
		_ = writer.WriteMsg(response)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()

	resolver, err := NewResolver(server.PacketConn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	response, err := resolver.Lookup(context.Background(), "example.com", dns.TypeA)
	if response == nil || !errors.Is(err, ErrNoQuestion) {
		t.Fatalf("got response=%v error=%v, want response and ErrNoQuestion", response, err)
	}
}

func TestResolver_LookupRejectsMismatchedQuestion(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(response *dns.Msg)
	}{
		{
			name: "multiple questions",
			mutate: func(response *dns.Msg) {
				response.Question = append(response.Question, response.Question[0])
			},
		},
		{
			name: "wrong name",
			mutate: func(response *dns.Msg) {
				response.Question[0].Name = "other.example."
			},
		},
		{
			name: "wrong type",
			mutate: func(response *dns.Msg) {
				response.Question[0].Qtype = dns.TypeAAAA
			},
		},
		{
			name: "wrong class",
			mutate: func(response *dns.Msg) {
				response.Question[0].Qclass = dns.ClassCHAOS
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, err := startMockUDPServer(func(writer dns.ResponseWriter, request *dns.Msg) {
				response := new(dns.Msg).SetReply(request)
				test.mutate(response)
				_ = writer.WriteMsg(response)
			})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Shutdown()

			resolver, err := NewResolver(server.PacketConn.LocalAddr().String())
			if err != nil {
				t.Fatal(err)
			}
			response, err := resolver.Lookup(context.Background(), "example.com", dns.TypeA)
			if response == nil || !errors.Is(err, ErrInvalidQuestion) {
				t.Fatalf("got response=%v error=%v, want response and ErrInvalidQuestion", response, err)
			}
		})
	}
}

func TestResolver_LookupTruncatedFallsBackToTCP(t *testing.T) {
	server, err := startMockDNSServer(func(writer dns.ResponseWriter, request *dns.Msg) {
		response := new(dns.Msg).SetReply(request)
		if strings.HasPrefix(writer.LocalAddr().Network(), "udp") {
			response.Truncated = true
		} else {
			response.Answer = []dns.RR{&dns.A{
				Hdr: dns.RR_Header{Name: request.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.ParseIP("192.0.2.2"),
			}}
		}
		_ = writer.WriteMsg(response)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()

	resolver, err := NewResolver(server.address)
	if err != nil {
		t.Fatal(err)
	}
	response, err := resolver.Lookup(context.Background(), "example.com", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	if response.Truncated || len(response.Answer) != 1 {
		t.Fatalf("expected complete TCP response, got %+v", response)
	}
}

func TestResolver_LookupTruncatedTCPFailureReturnsUDPResponse(t *testing.T) {
	server, err := startMockUDPServer(func(writer dns.ResponseWriter, request *dns.Msg) {
		response := new(dns.Msg).SetReply(request)
		response.Truncated = true
		_ = writer.WriteMsg(response)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()

	resolver, err := NewResolver(server.PacketConn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	response, err := resolver.Lookup(context.Background(), "example.com", dns.TypeA)
	if response == nil || !response.Truncated || err == nil {
		t.Fatalf("got response=%v error=%v, want truncated UDP response and TCP error", response, err)
	}
}
