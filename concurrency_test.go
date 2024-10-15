package fastresolver

import (
	"context"
	"testing"

	"github.com/miekg/dns"
)

func TestConcurrencyResolver_Lookup(t *testing.T) {
	r1, err := NewResolver("1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := NewResolver("8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	//bad server
	r3, err := NewResolver("0.1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	r := NewConcurrencyResolver(r1, r2, r3)
	ret, err := r.Lookup(context.Background(), "google.com", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", ret)
}
