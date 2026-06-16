package fastresolver

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestDefault(t *testing.T) {
	// 启动 Mock UDP DNS 服务器
	server, err := startMockUDPServer(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Authoritative = true
		if r.Question[0].Name == "dns.google." && r.Question[0].Qtype == dns.TypeAAAA {
			aaaa := &dns.AAAA{
				Hdr:  dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 300},
				AAAA: net.ParseIP("2001:4860:4860::8888"),
			}
			m.Answer = append(m.Answer, aaaa)
		}
		_ = w.WriteMsg(m)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()

	// 临时修改默认列表，指向本地端口
	oldFamous := defaultFamous
	defaultFamous = []string{server.PacketConn.LocalAddr().String()}
	defer func() {
		defaultFamous = oldFamous
	}()

	r := Default()
	rr, err := r.Lookup(context.Background(), "dns.google", dns.TypeAAAA)
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.AAAA) == 0 || rr.AAAA[0] != "2001:4860:4860::8888" {
		t.Fatalf("expected AAAA '2001:4860:4860::8888', got %v", rr.AAAA)
	}
}

func Test_cacheNetLookupIP(t *testing.T) {
	// 启动 Mock UDP 服务
	server, err := startMockUDPServer(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Authoritative = true
		
		if r.Question[0].Name == "dns.google." {
			if r.Question[0].Qtype == dns.TypeA {
				a := &dns.A{
					Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
					A:   net.ParseIP("8.8.8.8"),
				}
				m.Answer = append(m.Answer, a)
			} else if r.Question[0].Qtype == dns.TypeAAAA {
				aaaa := &dns.AAAA{
					Hdr:  dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 300},
					AAAA: net.ParseIP("2001:4860:4860::8888"),
				}
				m.Answer = append(m.Answer, aaaa)
			}
		}
		_ = w.WriteMsg(m)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()

	// 劫持 net.DefaultResolver 重定向到 Mock 端口
	oldResolver := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "udp", server.PacketConn.LocalAddr().String())
		},
	}
	defer func() {
		net.DefaultResolver = oldResolver
	}()

	// 重置全局缓存，确保测试隔离
	DefaultMemCache = NewLRU(50000, time.Minute)

	for i := 0; i < 10; i++ {
		rr, err := cacheNetLookupIP("dns.google")
		if err != nil {
			t.Fatal(err)
		}
		if len(rr.A) == 0 || rr.A[0] != "8.8.8.8" {
			t.Fatalf("expected A '8.8.8.8', got %v", rr.A)
		}
		if len(rr.AAAA) == 0 || rr.AAAA[0] != "2001:4860:4860::8888" {
			t.Fatalf("expected AAAA '2001:4860:4860::8888', got %v", rr.AAAA)
		}
	}
}
