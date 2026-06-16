package fastresolver

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestTimeoutResolver_Lookup_Normal(t *testing.T) {
	server, err := startMockUDPServer(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Authoritative = true
		if r.Question[0].Name == "google.com." && r.Question[0].Qtype == dns.TypeA {
			a := &dns.A{
				Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
				A:   net.ParseIP("1.2.3.4"),
			}
			m.Answer = append(m.Answer, a)
		}
		_ = w.WriteMsg(m)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()

	base, err := NewResolver(server.PacketConn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	r := NewTimeoutResolver(base, 5*time.Second)

	_, err = r.Lookup(context.Background(), "google.com", dns.TypeA)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestTimeoutResolver_Lookup_Timeout(t *testing.T) {
	server, err := startMockUDPServer(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		_ = w.WriteMsg(m)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()

	base, err := NewResolver(server.PacketConn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	r := NewTimeoutResolver(base, 1*time.Microsecond)

	_, err = r.Lookup(context.Background(), "google.com", dns.TypeA)
	if err == nil {
		t.Fatal("expected a timeout error, but got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded error, got %v", err)
	}
}

func TestResolver_WithCustomTimeout(t *testing.T) {
	server, err := startMockUDPServer(func(w dns.ResponseWriter, r *dns.Msg) {
		time.Sleep(50 * time.Millisecond)
		m := new(dns.Msg)
		m.SetReply(r)
		_ = w.WriteMsg(m)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()

	r, err := NewResolverWithTimeout(server.PacketConn.LocalAddr().String(), 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	_, err = r.Lookup(context.Background(), "google.com", dns.TypeA)
	if err == nil {
		t.Fatal("expected error due to short custom socket timeout, but got nil")
	}
}
