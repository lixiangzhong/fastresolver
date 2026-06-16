package fastresolver

import (
	"context"
	"net"
	"testing"

	"github.com/miekg/dns"
)

func TestRecursiveLookup(t *testing.T) {
	// 启动 Mock UDP DNS 服务器
	server, err := startMockUDPServer(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		
		// 模拟 evas.ai 的 NS 记录查询响应
		if r.Question[0].Name == "evas.ai." && r.Question[0].Qtype == dns.TypeNS {
			m.Authoritative = true
			ns := &dns.NS{
				Hdr: dns.RR_Header{Name: "evas.ai.", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 300},
				Ns:  "ns1.mock.dns.",
			}
			m.Answer = append(m.Answer, ns)
		}
		_ = w.WriteMsg(m)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()

	// 拦截包级变量 internalResolver，重定向到本地 Mock 节点
	oldInternal := internalResolver
	defer func() {
		internalResolver = oldInternal
	}()

	mockAddr := server.PacketConn.LocalAddr().String()
	host, port, _ := net.SplitHostPort(mockAddr)
	ip := net.ParseIP(host)

	mockInternal := &mockLookup{
		lookupFunc: func(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
			return DNSRR{
				A: []string{net.JoinHostPort(ip.String(), port)},
			}, nil
		},
	}
	internalResolver = mockInternal

	rr, err := RecursiveLookup(context.Background(), "evas.ai", dns.TypeNS)
	if err != nil {
		t.Fatal(err)
	}
	
	if len(rr.NS) == 0 || rr.NS[0] != "ns1.mock.dns." {
		t.Fatalf("expected NS 'ns1.mock.dns.', got %v", rr.NS)
	}
}

type mockLookup struct {
	lookupFunc func(ctx context.Context, name string, qtype uint16) (DNSRR, error)
}

func (m *mockLookup) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	return m.lookupFunc(ctx, name, qtype)
}
