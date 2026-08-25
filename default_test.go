package fastresolver

import (
	"context"
	"net"
	"testing"

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
	record, ok := firstRR[*dns.AAAA](rr.Answer)
	if !ok || record.AAAA.String() != "2001:4860:4860::8888" {
		t.Fatalf("expected AAAA '2001:4860:4860::8888', got %v", rr.Answer)
	}
}
