package fastresolver_test

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	fastresolver "github.com/lixiangzhong/fastresolver/v3"
	"github.com/miekg/dns"
)

func TestV3_ResolverReturnsTypedServerFailure(t *testing.T) {
	server, address := startV3UDPServer(t, func(writer dns.ResponseWriter, request *dns.Msg) {
		_ = writer.WriteMsg(new(dns.Msg).SetRcode(request, dns.RcodeServerFailure))
	})
	defer server.Shutdown()
	resolver, err := fastresolver.NewResolver(address)
	if err != nil {
		t.Fatal(err)
	}

	response, err := resolver.Lookup(context.Background(), "servfail.example", dns.TypeA)
	var serverFailure fastresolver.ServerFailureError
	if response == nil || !errors.As(err, &serverFailure) {
		t.Fatalf("got response=%v error=%v, want response and ServerFailureError", response, err)
	}
}

func TestV3_RetryResolverRetriesServerFailure(t *testing.T) {
	var calls atomic.Uint64
	server, address := startV3UDPServer(t, func(writer dns.ResponseWriter, request *dns.Msg) {
		response := new(dns.Msg).SetReply(request)
		if calls.Add(1) == 1 {
			response.Rcode = dns.RcodeServerFailure
		}
		_ = writer.WriteMsg(response)
	})
	defer server.Shutdown()
	base, err := fastresolver.NewResolver(address)
	if err != nil {
		t.Fatal(err)
	}
	resolver := fastresolver.NewRetryResolver(2, base)

	response, err := resolver.Lookup(context.Background(), "retry.example", dns.TypeA)
	if err != nil || response == nil {
		t.Fatalf("got response=%v error=%v, want successful retry", response, err)
	}
	if gotCalls := calls.Load(); gotCalls != 2 {
		t.Fatalf("server received %d requests, want 2", gotCalls)
	}
}

func startV3UDPServer(
	t *testing.T,
	handler func(writer dns.ResponseWriter, request *dns.Msg),
) (*dns.Server, string) {
	t.Helper()
	packetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &dns.Server{
		PacketConn: packetConn,
		Handler:    dns.HandlerFunc(handler),
	}
	go func() { _ = server.ActivateAndServe() }()
	return server, packetConn.LocalAddr().String()
}
