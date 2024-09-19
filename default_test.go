package fastresolver

import (
	"context"
	"testing"

	"github.com/miekg/dns"
)

func TestDefault(t *testing.T) {
	r := Default()
	rr, err := r.Lookup(context.Background(), "dns.google", dns.TypeAAAA)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", rr)
}

func Test_cacheNetLookupIP(t *testing.T) {
	for i := 0; i < 10; i++ {
		rr, err := cacheNetLookupIP("dns.google")
		t.Log(rr, err)
	}
}
