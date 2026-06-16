package fastresolver

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestNewCustomResolver_Order(t *testing.T) {
	// -------------------------------------------------------------------------
	// 顺序 1：全局超时在重试机制外层 [Timeout(30ms) -> Retry(3)]
	// 整个 Lookup 调用被限制在最多 30ms 内。
	// Mock 服务会丢弃前 2 次查询，导致它们等待超时。
	// 因为第 1 次尝试在 30ms 时发生超时，全局超时机制被触发并取消了 Context，
	// 导致后续的重试无法再成功（因为 Context 已被取消，整个生命周期结束）。
	// -------------------------------------------------------------------------
	var callCount1 int32
	server1, err := startMockUDPServer(func(w dns.ResponseWriter, r *dns.Msg) {
		count := atomic.AddInt32(&callCount1, 1)
		if count <= 2 {
			// Drop the query to force a read timeout
			return
		}
		time.Sleep(10 * time.Millisecond)
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
	defer server1.Shutdown()

	base1, err := NewResolver(server1.PacketConn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}

	resolver1 := NewCustomResolverFromBase(
		base1,
		WithTimeoutResolver(30*time.Millisecond), // Outermost
		WithRetry(3), // Innermost
	)

	_, err = resolver1.Lookup(context.Background(), "google.com", dns.TypeA)
	if err == nil {
		t.Error("expected timeout error for Order 1, but got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}

	// -------------------------------------------------------------------------
	// 顺序 2：单次尝试超时在重试机制内层 [Retry(3) -> Timeout(30ms)]
	// 每次单独的 DNS 查询尝试都被限制在最多 30ms 内。
	// 第 1 次尝试在 30ms 时发生超时，向外层的 Retry 返回 Deadline Exceeded 错误。
	// 第 2 次尝试也在 30ms 时发生超时，向外层的 Retry 返回 Deadline Exceeded 错误。
	// 第 3 次尝试在 10ms 内成功返回。整个 Lookup 调用在大约 70ms 内成功完成。
	// -------------------------------------------------------------------------
	var callCount2 int32
	server2, err := startMockUDPServer(func(w dns.ResponseWriter, r *dns.Msg) {
		count := atomic.AddInt32(&callCount2, 1)
		if count <= 2 {
			// Drop the query to force a read timeout
			return
		}
		time.Sleep(10 * time.Millisecond)
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
	defer server2.Shutdown()

	base2, err := NewResolver(server2.PacketConn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}

	resolver2 := NewCustomResolverFromBase(
		base2,
		WithRetry(3), // Outermost
		WithTimeoutResolver(30*time.Millisecond), // Innermost
	)

	_, err = resolver2.Lookup(context.Background(), "google.com", dns.TypeA)
	if err != nil {
		t.Errorf("expected no error for Order 2, got %v", err)
	}
}
