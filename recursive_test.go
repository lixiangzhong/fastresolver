package fastresolver

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestRecursiveLookup_NativeAuthoritativeResponse(t *testing.T) {
	restoreRecursiveGlobals(t)
	internalResolver = lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		response := newTestResponse(name, qtype)
		response.Answer = []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   net.ParseIP("192.0.2.53"),
		}}
		return response, nil
	})
	want := newTestResponse("evas.ai", dns.TypeNS)
	want.Authoritative = true
	want.Answer = []dns.RR{&dns.NS{
		Hdr: dns.RR_Header{Name: "evas.ai.", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 300},
		Ns:  "ns1.example.",
	}}
	factory := func(server string) (ILookup, error) {
		return lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) { return want, nil }), nil
	}

	response, err := recursiveLookup(context.Background(), "evas.ai", dns.TypeNS, factory)
	if err != nil || response != want || len(response.Answer) != 1 {
		t.Fatalf("got response=%v error=%v", response, err)
	}
}

func TestRecursiveLookup_ReferralNSStaysInAuthority(t *testing.T) {
	restoreRecursiveGlobals(t)
	internalResolver = addressResponseResolver()
	referral := newTestResponse("evas.ai", dns.TypeNS)
	referral.Ns = []dns.RR{&dns.NS{
		Hdr: dns.RR_Header{Name: "evas.ai.", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 300},
		Ns:  "ns1.example.",
	}}
	factory := func(server string) (ILookup, error) {
		return lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) { return referral, nil }), nil
	}

	response, err := recursiveLookup(context.Background(), "evas.ai", dns.TypeNS, factory)
	if err != nil || response != referral || len(response.Answer) != 0 || len(response.Ns) != 1 {
		t.Fatalf("authority records were moved or lost: response=%v error=%v", response, err)
	}
}

func TestRecursiveLookup_PreservesSOATerminationHeuristic(t *testing.T) {
	response := newTestResponse("missing.example.com", dns.TypeA)
	response.Ns = []dns.RR{&dns.SOA{
		Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 60},
		Ns:  "ns1.example.com.",
	}}
	if !isTerminalNegative(response, "missing.example.com", dns.TypeA) {
		t.Fatal("expected legacy SOA heuristic to terminate recursion")
	}
	if response.Rcode != dns.RcodeSuccess {
		t.Fatalf("heuristic mutated native rcode: %d", response.Rcode)
	}
}

func TestRecursiveLookup_PublicZoneMissReturnsNXDomain(t *testing.T) {
	restoreRecursiveGlobals(t)
	response, err := RecursiveLookup(context.Background(), "not-a-public-zone.invalid", dns.TypeA)
	if err != nil || response == nil || response.Rcode != dns.RcodeNameError || !response.Response || len(response.Question) != 1 {
		t.Fatalf("got response=%v error=%v", response, err)
	}
}

func TestRecursiveLookup_MaxDepthReturnsLastResponse(t *testing.T) {
	restoreRecursiveGlobals(t)
	internalResolver = addressResponseResolver()
	var last *dns.Msg
	factory := func(server string) (ILookup, error) {
		return lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
			last = newTestResponse(name, qtype)
			last.Ns = []dns.RR{&dns.NS{
				Hdr: dns.RR_Header{Name: "ai.", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 300},
				Ns:  "next.example.",
			}}
			return last, nil
		}), nil
	}

	response, err := recursiveLookup(context.Background(), "evas.ai", dns.TypeA, factory)
	if response != last || !errors.Is(err, ErrMaxRecursionDepth) {
		t.Fatalf("got response=%p error=%v, want last=%p and max depth", response, err, last)
	}
}

func restoreRecursiveGlobals(t *testing.T) {
	t.Helper()
	oldInternal := internalResolver
	oldCache := cacheForRecursive
	cacheForRecursive = NewLRU(100, time.Minute)
	t.Cleanup(func() {
		internalResolver = oldInternal
		cacheForRecursive = oldCache
	})
}

func addressResponseResolver() ILookup {
	return lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		response := newTestResponse(name, qtype)
		response.Answer = []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   net.ParseIP("192.0.2.53"),
		}}
		return response, nil
	})
}
