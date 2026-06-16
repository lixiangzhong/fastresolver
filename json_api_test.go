package fastresolver

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestJSONAPI_Lookup(t *testing.T) {

	// r := NewJSONAPI("https://doh.pub/resolve", time.Second*3)
	r := NewJSONAPI("https://dns.alidns.com/resolve", time.Second*3)

	rr, err := r.Lookup(context.Background(), "cl.app", dns.TypeNS)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#+v", rr)
}

func TestJSONAPI_WithCustomClient(t *testing.T) {
	tr := &trackingRoundTripper{}
	client := &http.Client{
		Transport: tr,
		Timeout:   time.Second * 5,
	}
	r := NewJSONAPIWithClient("https://dns.alidns.com/resolve", client)
	_, _ = r.Lookup(context.Background(), "cl.app", dns.TypeNS)

	if !tr.called {
		t.Error("expected custom client's RoundTrip to be called, but it was not")
	}
}
