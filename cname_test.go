package fastresolver

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/miekg/dns"
)

func TestFollowCnameResolver_FollowsSupportedTypes(t *testing.T) {
	for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA, dns.TypePTR} {
		t.Run(dns.Type(qtype).String(), func(t *testing.T) {
			var calls int
			final := newTestResponse("target.example", qtype)
			resolver := NewFollowCnameResolver(lookupFunc(func(ctx context.Context, name string, gotType uint16) (*dns.Msg, error) {
				calls++
				if calls == 1 {
					response := newTestResponse(name, gotType)
					response.Answer = []dns.RR{&dns.CNAME{
						Hdr:    dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60},
						Target: "target.example.",
					}}
					return response, nil
				}
				return final, nil
			}))

			response, err := resolver.Lookup(context.Background(), "alias.example", qtype)
			if err != nil || response != final || calls != 2 {
				t.Fatalf("got response=%p error=%v calls=%d", response, err, calls)
			}
		})
	}
}

func TestFollowCnameResolver_DoesNotFollowWhenTargetTypeExists(t *testing.T) {
	response := newTestResponse("alias.example", dns.TypeA)
	response.Answer = []dns.RR{
		&dns.CNAME{Hdr: dns.RR_Header{Name: "alias.example.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60}, Target: "target.example."},
		&dns.A{Hdr: dns.RR_Header{Name: "target.example.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.ParseIP("192.0.2.40")},
	}
	resolver := NewFollowCnameResolver(lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		return response, nil
	}))

	got, err := resolver.Lookup(context.Background(), "alias.example", dns.TypeA)
	if err != nil || got != response {
		t.Fatalf("got response=%p error=%v, want original=%p", got, err, response)
	}
}

func TestFollowCnameResolver_IgnoresUnsupportedSectionsAndTypes(t *testing.T) {
	tests := []struct {
		name     string
		qtype    uint16
		inAnswer bool
	}{
		{name: "NS never follows", qtype: dns.TypeNS, inAnswer: true},
		{name: "TXT never follows", qtype: dns.TypeTXT, inAnswer: true},
		{name: "authority CNAME ignored", qtype: dns.TypeA},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			response := newTestResponse("alias.example", test.qtype)
			cname := &dns.CNAME{Hdr: dns.RR_Header{Name: "alias.example.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60}, Target: "target.example."}
			if test.inAnswer {
				response.Answer = []dns.RR{cname}
			} else {
				response.Ns = []dns.RR{cname}
			}
			resolver := NewFollowCnameResolver(lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
				calls++
				return response, nil
			}))
			got, err := resolver.Lookup(context.Background(), "alias.example", test.qtype)
			if err != nil || got != response || calls != 1 {
				t.Fatalf("got response=%p error=%v calls=%d", got, err, calls)
			}
		})
	}
}

func TestFollowCnameResolver_UsesFirstCname(t *testing.T) {
	var queried string
	resolver := NewFollowCnameResolver(lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		if queried == "" {
			queried = name
			response := newTestResponse(name, qtype)
			response.Answer = []dns.RR{
				&dns.CNAME{Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60}, Target: "first.example."},
				&dns.CNAME{Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60}, Target: "second.example."},
			}
			return response, nil
		}
		queried = name
		return newTestResponse(name, qtype), nil
	}))

	_, err := resolver.Lookup(context.Background(), "alias.example", dns.TypeA)
	if err != nil || queried != "first.example." {
		t.Fatalf("got queried=%q error=%v", queried, err)
	}
}

func TestFollowCnameResolver_LookupStopsOnCnameLoop(t *testing.T) {
	var calls int
	var last *dns.Msg
	resolver := NewFollowCnameResolver(lookupFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		calls++
		last = newTestResponse(name, qtype)
		last.Answer = []dns.RR{&dns.CNAME{
			Hdr:    dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60},
			Target: dns.Fqdn(name),
		}}
		return last, nil
	}))

	response, err := resolver.Lookup(context.Background(), "loop.example", dns.TypeA)
	if response != last || !errors.Is(err, ErrCnameDepthExceeded) || calls != defaultCnameFollowMaxDepth+1 {
		t.Fatalf("got response=%p error=%v calls=%d", response, err, calls)
	}
}
