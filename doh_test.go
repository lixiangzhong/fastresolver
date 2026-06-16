package fastresolver

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestDoH_Lookup(t *testing.T) {
	r := NewDoH("https://dns.alidns.com/dns-query", time.Second*3)
	rr, err := r.Lookup(context.Background(), "cl.app", dns.TypeNS)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#+v", rr)
}

type trackingRoundTripper struct {
	called bool
}

func (tr *trackingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.called = true
	return http.DefaultTransport.RoundTrip(req)
}

func TestDoH_WithCustomClient(t *testing.T) {
	tr := &trackingRoundTripper{}
	client := &http.Client{
		Transport: tr,
		Timeout:   time.Second * 5,
	}
	r := NewDoHWithClient("https://dns.alidns.com/dns-query", client)
	_, _ = r.Lookup(context.Background(), "cl.app", dns.TypeNS)

	if !tr.called {
		t.Error("expected custom client's RoundTrip to be called, but it was not")
	}
}
