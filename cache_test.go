package fastresolver

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

type mockSlowResolver struct {
	calls atomic.Int64
}

func (resolver *mockSlowResolver) Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	resolver.calls.Add(1)
	time.Sleep(20 * time.Millisecond)
	response := newTestResponse(name, qtype)
	response.Answer = []dns.RR{&dns.A{
		Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("192.0.2.30"),
	}}
	return response, nil
}

func TestCacheResolver_SingleflightIsolation(t *testing.T) {
	underlying := &mockSlowResolver{}
	resolver := NewCacheResolver(NewLRU(10, 5*time.Minute), underlying)

	const concurrentCount = 100
	responses := make([]*dns.Msg, concurrentCount)
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrentCount)
	for index := 0; index < concurrentCount; index++ {
		index := index
		go func() {
			defer waitGroup.Done()
			response, err := resolver.Lookup(context.Background(), "test.example", dns.TypeA)
			if err != nil {
				t.Errorf("Lookup failed: %v", err)
				return
			}
			responses[index] = response
		}()
	}
	waitGroup.Wait()

	if calls := underlying.calls.Load(); calls != 1 {
		t.Fatalf("expected one underlying lookup, got %d", calls)
	}
	seen := make(map[*dns.Msg]struct{}, concurrentCount)
	for _, response := range responses {
		if response == nil {
			t.Fatal("received nil response")
		}
		seen[response] = struct{}{}
	}
	if len(seen) != concurrentCount {
		t.Fatalf("expected %d independent responses, got %d", concurrentCount, len(seen))
	}

	responses[0].Answer[0].Header().Ttl = 1
	cached, err := resolver.Lookup(context.Background(), "test.example", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	if cached.Answer[0].Header().Ttl != 300 {
		t.Fatalf("caller mutation polluted cache: %v", cached.Answer)
	}
}

func TestMemLRU_SetAndGetCopy(t *testing.T) {
	cache := NewLRU(10, time.Minute)
	response := newTestResponse("example.com", dns.TypeA)
	response.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.ParseIP("192.0.2.31")}}

	cache.Set("example.com", dns.TypeA, response)
	response.Answer[0].Header().Ttl = 1
	first, ok := cache.Get("example.com", dns.TypeA)
	if !ok || first.Answer[0].Header().Ttl != 300 {
		t.Fatalf("Set retained caller-owned state: %v", first)
	}
	first.Answer[0].Header().Ttl = 2
	second, ok := cache.Get("example.com", dns.TypeA)
	if !ok || second.Answer[0].Header().Ttl != 300 || first == second {
		t.Fatalf("Get exposed cache-owned state: first=%v second=%v", first, second)
	}
}

func TestMemLRU_TTLPolicy(t *testing.T) {
	fixedNow := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name       string
		response   *dns.Msg
		options    CacheOptions
		wantTTL    time.Duration
		wantCached bool
	}{
		{name: "positive", response: responseWithTTL(300), wantTTL: 5 * time.Minute, wantCached: true},
		{name: "minimum override", response: responseWithTTL(1), options: CacheOptions{MinTTL: time.Minute}, wantTTL: time.Minute, wantCached: true},
		{name: "maximum override", response: responseWithTTL(300), options: CacheOptions{MaxTTL: 2 * time.Minute}, wantTTL: 2 * time.Minute, wantCached: true},
		{name: "zero ttl", response: responseWithTTL(0)},
		{name: "empty address response", response: newTestResponse("example.com", dns.TypeA)},
		{name: "truncated", response: truncatedResponse()},
		{name: "refused", response: responseWithRcode(dns.RcodeRefused)},
		{name: "servfail capped", response: responseWithRcode(dns.RcodeServerFailure), options: CacheOptions{DefaultTTL: 2 * time.Minute}, wantTTL: 30 * time.Second, wantCached: true},
		{name: "servfail ignores high minimum", response: responseWithRcode(dns.RcodeServerFailure), options: CacheOptions{DefaultTTL: time.Second, MinTTL: time.Minute}, wantTTL: 30 * time.Second, wantCached: true},
		{name: "nxdomain soa minimum", response: negativeResponse(dns.RcodeNameError, 120, 30), wantTTL: 30 * time.Second, wantCached: true},
		{name: "nodata soa minimum", response: negativeResponse(dns.RcodeSuccess, 120, 45), wantTTL: 45 * time.Second, wantCached: true},
		{name: "negative referral", response: negativeReferralResponse()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			created, err := NewLRUWithOptions(1, test.options)
			if err != nil {
				t.Fatal(err)
			}
			cache := created.(*memLRU)
			cache.now = func() time.Time { return fixedNow }
			cache.Set("example.com", dns.TypeA, test.response)
			item, ok := cache.cache.Peek(cacheKey{name: "example.com", qtype: dns.TypeA})
			if ok != test.wantCached {
				t.Fatalf("cache presence=%t, want %t", ok, test.wantCached)
			}
			if !ok {
				return
			}
			if gotTTL := item.expiredAt.Sub(fixedNow); gotTTL != test.wantTTL {
				t.Fatalf("got expiry %v, want %v", gotTTL, test.wantTTL)
			}
		})
	}
}

func TestMemLRU_DecrementsTTLOnHit(t *testing.T) {
	created, err := NewLRUWithOptions(1, CacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cache := created.(*memLRU)
	now := time.Unix(1_700_000_000, 0)
	cache.now = func() time.Time { return now }
	response := responseWithTTL(120)
	response.SetEdns0(1232, true)
	originalOPTTTL := response.IsEdns0().Hdr.Ttl
	cache.Set("example.com", dns.TypeA, response)

	now = now.Add(30 * time.Second)
	cached, ok := cache.Get("example.com", dns.TypeA)
	if !ok {
		t.Fatal("cache miss")
	}
	if got := cached.Answer[0].Header().Ttl; got != 90 {
		t.Fatalf("got answer TTL %d, want 90", got)
	}
	if got := cached.IsEdns0().Hdr.Ttl; got != originalOPTTTL {
		t.Fatalf("OPT TTL changed from %d to %d", originalOPTTTL, got)
	}
}

func TestNewLRUWithOptions_RejectsInvalidTTLRange(t *testing.T) {
	_, err := NewLRUWithOptions(1, CacheOptions{MinTTL: 2 * time.Minute, MaxTTL: time.Minute})
	if err == nil {
		t.Fatal("expected invalid cache TTL range error")
	}
}

func responseWithTTL(ttl uint32) *dns.Msg {
	response := newTestResponse("example.com", dns.TypeA)
	response.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl}}}
	return response
}

func responseWithRcode(rcode int) *dns.Msg {
	response := newTestResponse("example.com", dns.TypeA)
	response.Rcode = rcode
	return response
}

func truncatedResponse() *dns.Msg {
	response := responseWithTTL(300)
	response.Truncated = true
	return response
}

func negativeResponse(rcode int, soaTTL, minimum uint32) *dns.Msg {
	response := responseWithRcode(rcode)
	response.Ns = []dns.RR{&dns.SOA{
		Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: soaTTL},
		Ns:     "ns1.example.com.",
		Mbox:   "hostmaster.example.com.",
		Minttl: minimum,
	}}
	return response
}

func negativeReferralResponse() *dns.Msg {
	response := negativeResponse(dns.RcodeNameError, 120, 30)
	response.Ns = append(response.Ns, &dns.NS{
		Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 120},
		Ns:  "ns1.example.com.",
	})
	return response
}
