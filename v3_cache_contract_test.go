package fastresolver_test

import (
	"net"
	"testing"
	"time"

	fastresolver "github.com/lixiangzhong/fastresolver/v3"
	"github.com/miekg/dns"
)

func TestV3_CacheHitReturnsRemainingTTL(t *testing.T) {
	cache := fastresolver.NewLRU(10, time.Minute)
	request := new(dns.Msg).SetQuestion("cache.example.", dns.TypeA)
	response := new(dns.Msg).SetReply(request)
	response.Answer = []dns.RR{&dns.A{
		Hdr: dns.RR_Header{
			Name:   "cache.example.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    2,
		},
		A: net.ParseIP("192.0.2.1"),
	}}
	cache.Set("cache.example", dns.TypeA, response)
	time.Sleep(1100 * time.Millisecond)

	cached, ok := cache.Get("cache.example", dns.TypeA)
	if !ok || len(cached.Answer) != 1 {
		t.Fatalf("got response=%v hit=%t, want one cached answer", cached, ok)
	}
	if ttl := cached.Answer[0].Header().Ttl; ttl != 1 {
		t.Fatalf("got cached TTL %d, want remaining TTL 1", ttl)
	}
}
